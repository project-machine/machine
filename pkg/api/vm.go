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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/raharper/qcli"

	log "github.com/sirupsen/logrus"
)

type VMState int

const (
	VMInit VMState = iota
	VMStarted
	VMStopped
	VMFailed
	VMCleaned
)

func (v VMState) String() string {
	switch v {
	case VMInit:
		return "initialized"
	case VMStarted:
		return "started"
	case VMStopped:
		return "stopped"
	case VMFailed:
		return "failed"
	case VMCleaned:
		return "cleaned"
	default:
		return fmt.Sprintf("unknown VMState %d", v)
	}
}

type VMDef struct {
	Name       string     `yaml:"name"`
	Cpus       uint32     `yaml:"cpus" default:1`
	Memory     uint32     `yaml:"memory"`
	Serial     string     `yaml:"serial"`
	Nics       []NicDef   `yaml:"nics"`
	Disks      []QemuDisk `yaml:"disks"`
	Boot       string     `yaml:"boot"`
	Cdrom      string     `yaml:"cdrom"`
	UEFIVars   string     `yaml:"uefi-vars"`
	TPM        bool       `yaml:"tpm"`
	TPMVersion string     `yaml:"tpm-version"`
	SecureBoot bool       `yaml:"secure-boot"`
	Gui        bool       `yaml:"gui"`
}

func (v *VMDef) adjustDiskBootIdx(qti *qcli.QemuTypeIndex) ([]string, error) {
	allocated := []string{}
	// do this in two loops, first, if BootIndex is set in the disk, allocate
	// the bit in qti, next, for any disk without BootIndex set, allocate new
	// bootindex from qti

	// mark any configured
	for n := range v.Disks {
		disk := v.Disks[n]
		if disk.BootIndex != "" {
			log.Infof("disk: setting configured index %s on %s", disk.BootIndex, disk.File)
			bootindex, err := strconv.Atoi(disk.BootIndex)
			if err != nil {
				return allocated, fmt.Errorf("Failed to parse disk %s BootIndex '%s' as integer: %s", disk.File, disk.BootIndex, err)
			}
			if err := qti.SetBootIndex(bootindex); err != nil {
				return allocated, fmt.Errorf("Failed to set BootIndex %s on disk %s: %s", disk.BootIndex, disk.File, err)
			}
			allocated = append(allocated, disk.BootIndex)
		}
	}

	// for any disks without a BootIndex, allocate one
	for n := range v.Disks {
		disk := v.Disks[n]
		if disk.BootIndex == "" {
			idx := qti.NextBootIndex()
			disk.BootIndex = fmt.Sprintf("%d", idx)
			log.Infof("disk: allocating new index %d on %s", idx, disk.File)
			allocated = append(allocated, disk.BootIndex)
			v.Disks[n] = disk
		}
	}

	return allocated, nil
}

func (v *VMDef) adjustNetBootIdx(qti *qcli.QemuTypeIndex) ([]string, error) {
	allocated := []string{}

	// do this in two loops, first, if BootIndex is set in the nic, allocate
	// the bit in qti, next, for any nic without BootIndex set, allocate new
	// bootindex from qti

	// mark any configured
	for n := range v.Nics {
		nic := v.Nics[n]
		if nic.BootIndex != "" {
			log.Infof("nic: setting configured index %s on %s", nic.BootIndex, nic.ID)
			bootindex, err := strconv.Atoi(nic.BootIndex)
			if err != nil {
				return allocated, fmt.Errorf("Failed to parse nic %s BootIndex '%s' as integer: %s", nic.ID, nic.BootIndex, err)
			}
			if err := qti.SetBootIndex(bootindex); err != nil {
				return allocated, fmt.Errorf("Failed to set BootIndex %s on nic %s: %s", nic.BootIndex, nic.ID, err)
			}
			allocated = append(allocated, nic.BootIndex)
		}
	}

	// for any nics without a BootIndex, allocate one
	for n := range v.Nics {
		nic := v.Nics[n]
		if nic.BootIndex == "" {
			idx := qti.NextBootIndex()
			nic.BootIndex = fmt.Sprintf("%d", idx)
			log.Infof("nic: allocating new index %d on %s", idx, nic.ID)
			allocated = append(allocated, nic.BootIndex)
			v.Nics[n] = nic
		}
	}

	return allocated, nil
}

func (v *VMDef) AdjustBootIndicies(qti *qcli.QemuTypeIndex) error {

	_, err := v.adjustDiskBootIdx(qti)
	if err != nil {
		return fmt.Errorf("Error setting disk bootindex values: %s", err)
	}

	_, err = v.adjustNetBootIdx(qti)
	if err != nil {
		return fmt.Errorf("Error setting nic bootindex values: %s", err)
	}

	return nil
}

// TODO: Rename fields
type VM struct {
	Ctx     context.Context
	Cancel  context.CancelFunc
	Config  VMDef
	State   VMState
	RunDir  string
	sockDir string
	Cmd     *exec.Cmd
	SwTPM   *SwTPM
	qcli    *qcli.Config
	qmp     *qcli.QMP
	qmpCh   chan struct{}
	wg      sync.WaitGroup
}

// note VM.sockDir is the path to the real sockets and runDir/sockets is a symlink to the socket
func (v *VM) SocketDir() string {
	return filepath.Join(v.RunDir, "sockets")
}

func (v *VM) findCharDeviceByID(deviceID string) (qcli.CharDevice, error) {
	for _, chardev := range v.qcli.CharDevices {
		if chardev.ID == deviceID {
			return chardev, nil
		}
	}
	return qcli.CharDevice{}, fmt.Errorf("Failed to find a char device with id:%s", deviceID)
}

func (v *VM) MonitorSocket() (string, error) {
	devID := "monitor0"
	cdev, err := v.findCharDeviceByID(devID)
	if err != nil {
		return "", fmt.Errorf("Failed to find a monitor device with id=%s: %s", devID, err)
	}
	return cdev.Path, nil
}

func (v *VM) SerialSocket() (string, error) {
	log.Infof("VM.SerialSocket")
	devID := "serial0"
	cdev, err := v.findCharDeviceByID(devID)
	if err != nil {
		return "", fmt.Errorf("Failed to find a serial device with id=%s: %s", devID, err)
	}
	return cdev.Path, nil
}

func (v *VM) SpiceDevice() (qcli.SpiceDevice, error) {
	return v.qcli.SpiceDevice, nil
}

func (v *VM) TPMSocket() (string, error) {
	return v.qcli.TPM.Path, nil
}

func newVM(ctx context.Context, clusterName string, vmConfig VMDef) (*VM, error) {
	ctx, cancelFn := context.WithCancel(ctx)
	runDir := filepath.Join(ctx.Value(clsCtxStateDir).(string), vmConfig.Name)

	if !PathExists(runDir) {
		err := EnsureDir(runDir)
		if err != nil {
			return &VM{}, fmt.Errorf("Error creating VM run dir '%s': %s", runDir, err)
		}
	}

	// UNIX sockets cannot have a long path so:
	// 1. create a dir under /tmp to hold the real sockets, the VM will
	//    reference this path
	// 2. create a symlink, $runDir/sockets which points to the tmp dir
	// 3. the VM will use the tmp path, and the Machine will return the statedir
	// path to client
	tmpSockDir, err := GetTempSocketDir()
	if err != nil {
		return &VM{}, fmt.Errorf("Failed to create temp socket dir: %s", err)
	}

	sockLink := filepath.Join(runDir, "sockets")
	if err := ForceLink(tmpSockDir, sockLink); err != nil {
		return &VM{}, fmt.Errorf("Failed to link socket dir: %s", err)
	}

	log.Infof("newVM: Generating QEMU Config")
	qcfg, err := GenerateQConfig(runDir, tmpSockDir, vmConfig)
	if err != nil {
		return &VM{}, fmt.Errorf("Failed to generate qcli Config from VM definition: %s", err)
	}

	cmdParams, err := qcli.ConfigureParams(qcfg, nil)
	if err != nil {
		return &VM{}, fmt.Errorf("Failed to generate new VM command parameters: %s", err)
	}
	log.Infof("newVM: generated qcli config parameters: %s", cmdParams)

	return &VM{
		Config:  vmConfig,
		Ctx:     ctx,
		Cancel:  cancelFn,
		State:   VMInit,
		Cmd:     exec.CommandContext(ctx, qcfg.Path, cmdParams...),
		qcli:    qcfg,
		RunDir:  runDir,
		sockDir: tmpSockDir, // this must point to the /tmp path to remain short
	}, nil
}

func (v *VM) Name() string {
	return v.Config.Name
}

func (v *VM) runVM() error {
	// add to waitgroup and spawn goroutine to run the command
	errCh := make(chan error, 1)

	v.wg.Add(1)
	go func() {
		var stderr bytes.Buffer
		defer func() {
			v.wg.Done()
			if v.State != VMFailed {
				v.State = VMStopped
			}
		}()

		if v.Config.TPM {
			tpmDir := filepath.Join(v.RunDir, "tpm")
			if err := EnsureDir(tpmDir); err != nil {
				errCh <- fmt.Errorf("Failed to create tpm state dir: %s", err)
				return
			}
			tpmSocket, err := v.TPMSocket()
			if err != nil {
				errCh <- fmt.Errorf("Failed to get TPM Socket path: %s", err)
				return
			}
			v.SwTPM = &SwTPM{
				StateDir: tpmDir,
				Socket:   tpmSocket,
				Version:  v.Config.TPMVersion,
			}
			if err := v.SwTPM.Start(); err != nil {
				errCh <- fmt.Errorf("Failed to start SwTPM: %s", err)
				return
			}
		}

		log.Infof("VM:%s starting QEMU process", v.Name())
		v.Cmd.Stderr = &stderr
		err := v.Cmd.Start()
		if err != nil {
			errCh <- fmt.Errorf("VM:%s failed with: %s", stderr.String())
			return
		}

		v.State = VMStarted
		log.Infof("VM:%s waiting for QEMU process to exit...", v.Name())
		err = v.Cmd.Wait()
		if err != nil {
			errCh <- fmt.Errorf("VM:%s wait failed with: %s", v.Name(), stderr.String())
			return
		}
		log.Infof("VM:%s QEMU process exited", v.Name())
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Errorf("runVM failed: %s", err)
			v.State = VMFailed
			return err
		}
	}

	return nil
}

func (v *VM) StartQMP() error {
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	// FIXME: are there more than one qmp sockets allowed?
	numQMP := len(v.qcli.QMPSockets)
	if numQMP != 1 {
		return fmt.Errorf("StartQMP failed, expected 1 QMP socket, found: %d", numQMP)
	}

	// start qmp goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		// watch for qmp/monitor/serial sockets
		waitOn, err := qcli.GetSocketPaths(v.qcli)
		if err != nil {
			errCh <- fmt.Errorf("StartQMP failed to fetch VM socket paths: %s", err)
			return
		}

		// wait up to for 10 seconds for each.
		for _, sock := range waitOn {
			if !WaitForPath(sock, 10, 1) {
				errCh <- fmt.Errorf("VM:%s socket %s does not exist", v.Name(), sock)
				return
			}
		}

		qmpCfg := qcli.QMPConfig{
			Logger: QMPMachineLogger{},
		}

		qmpSocketFile := v.qcli.QMPSockets[0].Name
		attempt := 0
		for {
			qmpCh := make(chan struct{})
			attempt = attempt + 1
			log.Infof("VM:%s connecting to QMP socket %s attempt %d", v.Name(), qmpSocketFile, attempt)
			q, qver, err := qcli.QMPStart(v.Ctx, qmpSocketFile, qmpCfg, qmpCh)
			if err != nil {
				errCh <- fmt.Errorf("Failed to connect to qmp socket: %s, retrying...", err.Error())
				time.Sleep(time.Second * 1)
				continue
			}
			log.Infof("VM:%s QMP:%v QMPVersion:%v", v.Name(), q, qver)

			// This has to be the first command executed in a QMP session.
			err = q.ExecuteQMPCapabilities(v.Ctx)
			if err != nil {
				errCh <- err
				time.Sleep(time.Second * 1)
				continue
			}
			log.Infof("VM:%s QMP ready", v.Name())
			v.qmp = q
			v.qmpCh = qmpCh
			break
		}
		errCh <- nil
	}()

	// wait until qmp setup is complete (or failed)
	wg.Wait()

	select {
	case err := <-errCh:
		if err != nil {
			log.Errorf("StartQMP failed: %s", err)
			return err
		}
	}

	return nil
}

func (v *VM) BackgroundRun() error {

	// start vm command in background goroutine
	go func() {
		log.Infof("VM:%s backgrounding runVM()", v.Name())
		err := v.runVM()
		if err != nil {
			log.Errorf("runVM error: %s", err)
			return
		}
	}()

	go func() {
		log.Infof("VM:%s backgrounding StartQMP()", v.Name())
		err := v.StartQMP()
		if err != nil {
			log.Errorf("StartQMP error: %s", err)
			return
		}
	}()

	return nil
}

func (v *VM) QMPStatus() qcli.RunState {
	if v.qmp != nil {
		vmName := v.Name()
		log.Infof("VM:%s querying CPUInfo via QMP...", vmName)
		cpuInfo, err := v.qmp.ExecQueryCpus(context.TODO())
		if err != nil {
			return qcli.RunStateUnknown
		}
		log.Infof("VM:%s has %d CPUs", vmName, len(cpuInfo))

		log.Infof("VM:%s querying VM Status via QMP...", vmName)
		status, err := v.qmp.ExecuteQueryStatus(context.TODO())
		if err != nil {
			return qcli.RunStateUnknown
		}
		log.Infof("VM:%s Status:%s Running:%v", vmName, status.Status, status.Running)
		return qcli.ToRunState(status.Status)
	}
	log.Infof("VM:%s qmp socket is not ready yet", v.Name())
	return qcli.RunStateUnknown
}

func (v *VM) Status() VMState {
	return v.State
}

func (v *VM) Start() error {
	log.Infof("VM:%s starting...", v.Name())
	err := v.BackgroundRun()
	if err != nil {
		log.Errorf("VM:%s failed to start VM:%s %s", v.Name(), err)
		v.Stop(true)
		return err
	}
	v.QMPStatus()
	return nil
}

func (v *VM) Stop(force bool) error {
	pid := v.Cmd.Process.Pid
	status := v.Cmd.ProcessState.String()
	log.Infof("VM:%s PID:%d Status:%s Force:%v stopping...\n", v.Name(), pid, status, force)

	v.Status()

	if v.qmp != nil {
		log.Infof("VM:%s PID:%d qmp is not nill, sending qmp command", v.Name(), pid)
		// FIXME: configurable?
		// Try shutdown via QMP, wait up to 10 seconds before force shutting down
		timeout := time.Second * 10

		if force {
			// Let's force quit
			// send a quit message.
			log.Infof("VM:%s forcefully stopping vm via quit (%s timeout before cancelling)..", v.Name(), timeout.String())
			err := v.qmp.ExecuteQuit(v.Ctx)
			if err != nil {
				log.Errorf("VM:%s error:%s", v.Name(), err.Error())
			}
		} else {
			// Let's try to shutdown the VM.  If it hasn't shutdown in 10 seconds we'll
			// send a poweroff message.
			log.Infof("VM:%s trying graceful shutdown via system_powerdown (%s timeout before cancelling)..", v.Name(), timeout.String())
			err := v.qmp.ExecuteSystemPowerdown(v.Ctx)
			if err != nil {
				log.Errorf("VM:%s error:%s", v.Name(), err.Error())
			}
		}

		log.Infof("waiting on Ctx.Done() or time.After(timeout)")
		select {
		case <-v.qmpCh:
			log.Infof("VM:%s qmpCh.exited: has exited without cancel", v.Name())
		case <-v.Ctx.Done():
			log.Infof("VM:%s Ctx.Done(): has exited without cancel", v.Name())
		case <-time.After(timeout):
			log.Warnf("VM:%s timed out, killing via cancel context...", v.Name())
			v.Cancel()
			log.Warnf("VM:%s cancel() complete")
		}
		v.wg.Wait()
	} else {
		log.Infof("VM:%s PID:%d qmp is not set, killing pid...", v.Name(), pid)
		if err := v.Cmd.Process.Kill(); err != nil {
			log.Errorf("Error killing VM:%s PID:%d Error:%v", v.Name(), pid, err)
		}
	}

	if v.Config.TPM {
		v.SwTPM.Stop()
	}

	// when runVM goroutine exits, it marks v.State = VMStopped
	return nil
}

func (v *VM) IsRunning() bool {
	if v.State == VMStarted {
		return true
	}
	return false
}

func (v *VM) Delete() error {
	log.Infof("VM:%s deleting self...", v.Name())
	if v.IsRunning() {
		err := v.Stop(true)
		if err != nil {
			return fmt.Errorf("Failed to delete VM:%s :%s", v.Name(), err)
		}
	}

	if PathExists(v.RunDir) {
		log.Infof("VM:%s removing state dir: %q", v.Name(), v.RunDir)
		return os.RemoveAll(v.RunDir)
	}

	return nil
}
