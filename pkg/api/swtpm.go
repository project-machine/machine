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
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

type SwTPM struct {
	StateDir string
	Socket   string
	Version  string
	cmd      *exec.Cmd
	finished chan error
}

// ${StateDir}/swtpm-localca.conf
const swTPMLocalCaConf = "swtpm-localca.conf"
const swTPMLocalCaConfTpl = `
statedir = {{.StateDir}}
signingkey = {{.StateDir}}/signingkey.pem
issuercert = {{.StateDir}}/issuercert.pem
certserial = {{.StateDir}}/certserial
`

// ${StateDir}/swtpm-localca.options
const swTPMLocalCaOptions = "swtpm-localca.options"
const swTPMLocalCaOptionsTpl = `
--tpm-manufacturer IBM
--tpm-model swtpm-libtpms
--tpm-version {{.Version}}
--platform-manufacturer MachineOS
--platform-version 2.1
--platform-model QEMU
`

// ${StateDir}/swtpm_setup.conf
const swTPMSetupConf = "swtpm_setup.conf"
const swTPMSetupConfTpl = `
create_certs_tool={{.CertsTool}}
create_certs_tool_config={{.StateDir}}/swtpm-localca.conf
create_certs_tool_options={{.StateDir}}/swtpm-localca.options
`

var swTPMSetupTemplates = map[string]string{
	swTPMLocalCaConf:    swTPMLocalCaConfTpl,
	swTPMLocalCaOptions: swTPMLocalCaOptionsTpl,
	swTPMSetupConf:      swTPMSetupConfTpl,
}

func renderSwTPMTemplate(templateSource, filename string, data interface{}) error {
	log.Debugf("SwTPM: rendering template for %s", filename)
	tmpl, err := template.New(filename).Parse(templateSource)
	if err != nil {
		return fmt.Errorf("Failed to read template for %s: %s", filename, err)
	}

	var tmplBuffer bytes.Buffer
	err = tmpl.Execute(&tmplBuffer, data)
	if err != nil {
		return fmt.Errorf("Failed to render template for %s: %s", filename, err)
	}

	err = ioutil.WriteFile(filename, tmplBuffer.Bytes(), 0644)
	if err != nil {
		return fmt.Errorf("Failed to write template to file: %s: %s", filename, err)
	}
	return nil
}

func (s *SwTPM) Setup() error {
	if err := os.MkdirAll(s.StateDir, 0755); err != nil {
		return fmt.Errorf("SwTPM Setup failed to create statedir: %s: %s", s.StateDir, err)
	}

	// check if we've already setup a tpm before
	if PathExists(filepath.Join(s.StateDir, "tpm-00.permall")) {
		log.Debugf("SwTPM already configured, skipping setup")
		return nil
	}

	if Which("swtpm_setup") == "" {
		return fmt.Errorf("no 'swtpm_setup' command found in PATH.")
	}

	log.Infof("Checking swtpm_setup version ...")
	stdout, stderr, rc := RunCommandWithOutputErrorRc("swtpm_setup", "--version")
	// swtpm_setup returns 1 on --version flag ... *sigh*
	if rc != 1 {
		return fmt.Errorf("failed to run 'swtpm_setup --version', rc:%d stdout: %s, stderr: %s", rc, string(stdout), string(stderr))
	}

	// expected output from --version: TPM emulator setup tool version 0.7.1
	toks := strings.Split(strings.TrimSpace(string(stdout)), " ")
	swtpmVersion := toks[len(toks)-1]
	var major, minor, micro int
	numParsed, err := fmt.Sscanf(swtpmVersion, "%d.%d.%d", &major, &minor, &micro)
	if err != nil || numParsed != 3 {
		return fmt.Errorf("Failed to parse swtpm_setup version string '%s': %s", swtpmVersion, err)
	}
	log.Infof("Found swtpm_setup version string:%s major:%d minor:%d micro:%d", swtpmVersion, major, minor, micro)

	// For SecureBoot TPM 2.0 mode we skip swtpm_setup if version is older than 0.7.3 as it does not work
	if strings.HasPrefix(s.Version, "2") {
		if major == 0 && minor <= 7 && micro < 3 {
			log.Infof("Skipping swtpm_setup for TPM Version 2, SwTPM version %s is not >= 0.7.3", swtpmVersion)
			return nil
		}
	}

	certsTool := Which("swtpm_localca")
	if certsTool == "" {
		return fmt.Errorf("no 'swtpm_localca' command found in PATH.")
	}

	data := make(map[string]interface{})
	data["StateDir"] = s.StateDir
	data["Version"] = s.Version
	data["CertsTool"] = certsTool

	for fname, tpl := range swTPMSetupTemplates {
		if err := renderSwTPMTemplate(tpl, filepath.Join(s.StateDir, fname), data); err != nil {
			return fmt.Errorf("failed to render template for %s with data: %+v", fname, data)
		}
	}

	// swtpm_setup --help shows it accepts either '--tpmstate', or
	// '--tpm-state' but does not support '--tpm-state=dir://'
	// '--log=' works, but not with level= or file= values
	args := []string{
		"swtpm_setup",
		"--tpm-state", "dir://" + s.StateDir,
		"--config=" + filepath.Join(s.StateDir, swTPMSetupConf),
		"--log=" + path.Join(s.StateDir, "log"),
		"--createek", // not a a typo 'create ek'
		"--create-ek-cert",
		"--create-platform-cert",
		"--lock-nvram",
		"--not-overwrite",
		"--write-ek-cert-files=" + filepath.Join(s.StateDir),
	}

	// tpm1 mode requires well-known values set; these flags break tpm 2.0 secureboot mode
	if strings.HasPrefix(s.Version, "1") {
		args = append(args, "--srk-well-known", "--owner-well-known")
	}

	log.Infof("swtpm_setup args: %s", strings.Join(args, " "))
	stdout, stderr, rc = RunCommandWithOutputErrorRc(args...)
	if rc != 0 {
		return fmt.Errorf("failed to run 'swtpm_setup' rc:%d stdout:%s stderr:%s", rc, string(stdout), string(stderr))
	}
	return nil
}

func (s *SwTPM) Start() error {
	if Which("swtpm") == "" {
		return fmt.Errorf("no 'swtpm' command found in PATH.")
	}

	err := s.Setup()
	if err != nil {
		// swtpm_setup is mandatory for 1.2 tpms to function, 2.0 can proceed
		if strings.HasPrefix(s.Version, "1") {
			return fmt.Errorf("Cannot start SwTPM, required setup for TPM 1.x failed")
		}
		log.Warnf("SwTPM Setup() failed. Some TPM features may not function. Please update swtpm to 0.7.1 or newer")
	}

	// swtpm socket --help shows it accepts only '--tpmstate', not '--tpm-state'
	// note that --tpmstate does NOT accept dir:// like swtpm_setup does
	args := []string{
		"swtpm", "socket",
		"--tpmstate=dir=" + s.StateDir,
		"--ctrl=type=unixio,path=" + s.Socket,
		"--log=level=20,file=" + path.Join(s.StateDir, "log"),
		"--pid=file=" + path.Join(s.StateDir, "pid"),
	}

	if strings.HasPrefix(s.Version, "2") {
		args = append(args, "--tpm2")
	} else {
		// no args needed for tpm 1.2, it is the default chip version
		if !strings.HasPrefix(s.Version, "1") {
			return fmt.Errorf("Invalid SwTPM Version: '%s', must be 1.2 or 2.0", s.Version)
		}
	}

	cmd := exec.Command(args[0], args[1:]...)
	log.Infof("swtpm args: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		return err
	}

	// wait up to 10 seconds for the SwTPM socket to appear
	if !WaitForPath(s.Socket, 10, 1) {
		return fmt.Errorf("SwTPM start failed, socket %s does not exist after 10 seconds", s.Socket)
	}

	log.Infof("swtpm TPM Version %s started with pid %d", s.Version, cmd.Process.Pid)
	s.cmd = cmd

	go func() {
		s.finished <- s.cmd.Wait()
	}()

	return nil
}

func (s *SwTPM) Stop() error {
	// never started.
	if s.cmd == nil {
		return nil
	}

	pid := s.cmd.Process.Pid
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		log.Warnf("Failed to kill %d: %v", pid, err)
		return err
	}

	timeout := time.Duration(2) * time.Second
	select {
	case <-s.finished:
		log.Infof("swtpm pid %d exited after sigterm", pid)
	case <-time.After(timeout):
		log.Infof("SwTPM pid %d didn't die right away, killing.", pid)
		if err := s.cmd.Process.Kill(); err != nil {
			return err
		}
	}
	return nil
}
