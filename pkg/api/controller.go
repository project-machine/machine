/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/msoap/byline"
	log "github.com/sirupsen/logrus"
)

type Controller struct {
	Config            *MachineDaemonConfig
	Router            *gin.Engine
	MachineController MachineController
	Server            *http.Server
	wgShutDown        *sync.WaitGroup
	portNumber        int
}

func NewController(config *MachineDaemonConfig) *Controller {
	var controller Controller

	controller.Config = config
	controller.wgShutDown = new(sync.WaitGroup)

	return &controller
}

func (c *Controller) Run(ctx context.Context) error {
	// load existing machines
	machineDir := filepath.Join(c.Config.ConfigDirectory, "machines")
	if PathExists(machineDir) {
		log.Infof("Loading saved machine configs...")
		err := filepath.Walk(machineDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				machineConf := filepath.Join(path, "machine.yaml")
				if PathExists(machineConf) {
					newMachine, err := LoadConfig(machineConf)
					if err != nil {
						return err
					}
					newMachine.ctx = c.Config.GetConfigContext()
					log.Infof("  loaded machine %s", newMachine.Name)
					c.MachineController.Machines = append(c.MachineController.Machines, newMachine)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	unixSocket := APISocketPath()
	if len(unixSocket) == 0 {
		panic("Failed to get an API Socket path")
	}

	// mkdir -p on dirname(unixSocet)
	err := os.MkdirAll(filepath.Dir(unixSocket), 0755)
	if err != nil {
		panic(fmt.Sprintf("Failed to create directory path to: %s", unixSocket))
	}

	// FIXME to check if another machined is running/pidfile?, flock?
	if PathExists(unixSocket) {
		os.Remove(unixSocket)
	}
	defer os.Remove(unixSocket)

	log.Infof("machined service running on: %s\n", unixSocket)
	engine := gin.Default()
	c.Router = engine

	//  configure routes
	_ = NewRouteHandler(c)

	// re-implement gin.Engine.RunUnix() so we can set the context ourselves
	listener, err := net.Listen("unix", unixSocket)
	if err != nil {
		panic("Failed to create a unix socket listener")
	}
	defer listener.Close()

	c.Server = &http.Server{Handler: c.Router.Handler()}

	return c.Server.Serve(listener)
}

func (c *Controller) InitMachineController(ctx context.Context) error {
	c.MachineController = MachineController{}

	// TODO
	// look for serialized Machine configuration files in data dir
	// for each one, read them in and add to the Controller
	return nil
}

func (c *Controller) Shutdown(ctx context.Context) error {
	c.wgShutDown.Wait()
	if err := c.Server.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

//
// utility functions below here
//

func PathExists(d string) bool {
	_, err := os.Stat(d)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

func WaitForPath(path string, retries, sleepSeconds int) bool {
	var numRetries int
	if retries == 0 {
		numRetries = 1
	} else {
		numRetries = retries
	}
	for i := 0; i < numRetries; i++ {
		if PathExists(path) {
			return true
		}
		time.Sleep(time.Duration(sleepSeconds) * time.Second)
	}
	return PathExists(path)
}

func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("couldn't make dirs: %s", err)
	}
	return nil
}

func Which(commandName string) string {
	return WhichInRoot(commandName, "")
}

func WhichInRoot(commandName string, root string) string {
	cmd := []string{"sh", "-c", "command -v \"$0\"", commandName}
	if root != "" && root != "/" {
		cmd = append([]string{"chroot", root}, cmd...)
	}
	out, rc := RunCommandWithRc(cmd...)
	if rc == 0 {
		return strings.TrimSuffix(string(out), "\n")
	}
	if rc != 127 {
		log.Warnf("checking for %s exited unexpected value %d\n", commandName, rc)
	}
	return ""
}

func LogCommand(args ...string) error {
	return LogCommandWithFunc(log.Infof, args...)
}

func LogCommandDebug(args ...string) error {
	return LogCommandWithFunc(log.Debugf, args...)
}

func LogCommandWithFunc(logf func(string, ...interface{}), args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		logf("%s-fail | %s", err)
		return err
	}
	cmd.Stderr = cmd.Stdout
	err = cmd.Start()
	if err != nil {
		logf("%s-fail | %s", args[0], err)
		return err
	}
	pid := cmd.Process.Pid
	logf("|%d-start| %q", pid, args)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := byline.NewReader(stdoutPipe).Each(
			func(line []byte) {
				logf("|%d-out  | %s", pid, line[:len(line)-1])
			}).Discard()
		if err != nil {
			log.Fatalf("Unexpected %s", err)
		}
		wg.Done()
	}()

	wg.Wait()
	err = cmd.Wait()

	logf("|%d-exit | rc=%d", pid, GetCommandErrorRC(err))
	return err
}

// CopyFileBits - copy file content from a to b
// differs from CopyFile in:
//  - does not do permissions - new files created with 0644
//  - if src is a symlink, copies content, not link.
//  - does not invoke sh.
func CopyFileBits(src, dest string) error {
	if len(src) == 0 {
		return fmt.Errorf("Source file is empty string")
	}
	if len(dest) == 0 {
		return fmt.Errorf("Destination file is empty string")
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Failed to open source file %q: %s", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("Failed to open destination file %q", dest, err)
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("Failed while copying %q -> %q: %s", src, dest, err)
	}
	return out.Close()
}

// Copy one file to a new path, i.e. cp a b
func CopyFileRefSparse(src, dest string) error {
	if err := EnsureDir(filepath.Dir(src)); err != nil {
		return err
	}
	if err := EnsureDir(filepath.Dir(dest)); err != nil {
		return err
	}
	cmdtxt := fmt.Sprintf("cp --force --reflink=auto --sparse=auto %s %s", src, dest)
	return RunCommand("sh", "-c", cmdtxt)
}

func RunCommand(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s: %s", strings.Join(args, " "), err, string(output))
	}
	return nil
}

func RunCommandWithRc(args ...string) ([]byte, int) {
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	return out, GetCommandErrorRC(err)
}

func RunCommandWithOutputErrorRc(args ...string) ([]byte, []byte, int) {
	cmd := exec.Command(args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), GetCommandErrorRC(err)
}

func GetCommandErrorRCDefault(err error, rcError int) int {
	if err == nil {
		return 0
	}
	exitError, ok := err.(*exec.ExitError)
	if ok {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	log.Debugf("Unavailable return code for %s. returning %d", err, rcError)
	return rcError
}

func GetCommandErrorRC(err error) int {
	return GetCommandErrorRCDefault(err, 127)
}

func GetTempSocketDir() (string, error) {
	d, err := ioutil.TempDir("/tmp", "msockets-*")
	if err != nil {
		return "", nil
	}
	if err := checkSocketDir(d); err != nil {
		os.RemoveAll(d)
		return "", err
	}
	return d, nil
}

// LinuxUnixSocketMaxLen - 108 chars max for a unix socket path (including null byte).
const LinuxUnixSocketMaxLen int = 108

func checkSocketDir(sdir string) error {
	// just use this as a filename that might go there.
	fname := "monitor.socket"
	if len(sdir)+len(fname) >= LinuxUnixSocketMaxLen {
		return fmt.Errorf("dir %s is too long (%d) to hold a unix socket", sdir, len(sdir))
	}
	return nil
}

func ForceLink(oldname, newname string) error {
	if oldname == "" {
		return fmt.Errorf("empty string for parameter 'oldname'")
	}
	if newname == "" {
		return fmt.Errorf("empty string for parameter 'newname'")
	}
	if !PathExists(oldname) {
		return fmt.Errorf("Source file %s does not exist", oldname)
	}
	log.Debugf("forceLink oldname=%s newname=%s", oldname, newname)
	if err := os.Remove(newname); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Failed removing %s before linking to %s: %s", newname, oldname, err)
	}
	if err := os.Symlink(oldname, newname); err != nil {
		return fmt.Errorf("Failed linking %s -> %s: %s", oldname, newname, err)
	}
	if !PathExists(newname) {
		return fmt.Errorf("Failed to symlink %s -> %s", newname, oldname)
	}
	return nil
}
