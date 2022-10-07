package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/apex/log"
	"github.com/pkg/errors"
)


type VMState int

const (
	VMInit VMState = iota
	VMStarted
	VMStopped
	VMFailed
	VMCleaned
)

var finalErr error

type VMDef struct {
	Name               string     `yaml:"name"`
	Serial             string     `yaml:"serial"`
	Disks              []QemuDisk `yaml:"disks"`
	Boot               string     `yaml:"boot"`
	Cdrom              string     `yaml:"cdrom"`
	UefiVars           string     `yaml:"uefi-vars"`
	TPM                bool       `yaml:"tpm"`
	TPMVersion         string     `yaml:"tpm-version"`
	KVMExtraOpts       []string   `yaml:"extra-opts"`
	SecureBoot         bool       `yaml:"secure-boot"`
	Gui                bool       `yaml:"gui"`
}

type KVMBootSource int

const (
	BootCD KVMBootSource = iota
	BootNet
	BootHDD
	BootSSD
	BootUnspec
)

func ParseKVMBootSource(s string) KVMBootSource {
	switch s {
	case "cd", "cdrom", "CDROM", "d":
		return BootCD
	case "n", "net", "network":
		return BootNet
	case "HDD", "hdd", "disk", "c", "C":
		return BootHDD
	case "SSD", "ssd":
		return BootSSD
	default:
		return BootUnspec
	}
}


type KVMRunOpts struct {
	Name           string
	Cpus           int
	Memory         int
	SecureBoot     bool
	ISO            string
	Boot           KVMBootSource
	Dir            string
	KVMExtraOpts   []string
	UefiVars       string
	UseTPM         bool
	TPMVersion     string
	Disks          []QemuDisk
	sockDir        string
	SwTPM          *SwTPM
	PciBusSlots    PciBus
	Gui            bool
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

func (opts KVMRunOpts) getSockDir() (string, error) {
	sdir := ""
	if opts.sockDir != "" {
		sdir = opts.sockDir
	} else if opts.Dir != "" {
		sdir = path.Join(opts.Dir, "sockets")
	} else {
		return sdir, errors.Errorf("Using monitor or serial socket requires KVMRunOpts.sockDir")
	}

	if err := checkSocketDir(sdir); err != nil {
		return sdir, err
	}

	return sdir, nil
}

func (opts KVMRunOpts) MonitorSocket() (string, error) {
	sockDir, err := opts.getSockDir()
	if err != nil {
		return "", err
	}
	return path.Join(sockDir, "monitor.socket"), nil
}

func (opts KVMRunOpts) ConsoleSocket() (string, error) {
	sockDir, err := opts.getSockDir()
	if err != nil {
		return "", err
	}
	return path.Join(sockDir, "console.socket"), nil
}

func (opts KVMRunOpts) TPMSocket() (string, error) {
	sockDir, err := opts.getSockDir()
	if err != nil {
		return "", err
	}
	return path.Join(sockDir, "tpm.socket"), nil
}


type VM struct {
	Name               string
	Boot               string
	endCh              chan struct{}
	doneCh             chan struct{}
	swtpmDiedCh        chan struct{}
	opts               KVMRunOpts
	cmd                *exec.Cmd
	KVMExtraOpts       []string
	State              VMState
	Cdrom              string
	Cpus               int
	Memory             int
	SecureBoot         bool
}

// 32 slots available, place auto-created devices
// starting at the end and decrement as needed
// to avoid impacting
const PCISlotMax int = 32

// slot 0, 1 and 2 are always taken
const PCISlotOffset = 3

type PciBus [PCISlotMax]bool

func setPciBusSlot(bus *PciBus, slot int) error {
	if slot > PCISlotMax {
		return errors.Errorf("Slot %d must be < %d", slot, PCISlotMax)
	}
	bus[slot] = true
	return nil
}

func getPciBusSlot(bus *PciBus) int {
	// skip to the starting slot offset
	for slot := PCISlotOffset; slot < PCISlotMax; slot++ {
		status := bus[slot]
		if !status {
			if err := setPciBusSlot(bus, slot); err != nil {
				log.Fatalf("Could not set PCI Bus slot: %v", err)
			}
			return slot
		}
	}
	errors.Errorf("No PCI slots remaining")
	return -1
}

func newVM(endCh chan struct{}, doneCh chan struct{}, vm VMDef, runDir string) (VM, error) {
	var err error
	var slot int

	cwd, err := os.Getwd()
	if err != nil {
		return VM{}, err
	}

	if vm.Cdrom != "" && !strings.Contains(vm.Cdrom, "/") {
		vm.Cdrom = path.Join(cwd, vm.Cdrom)
	}
	if runDir == "" {
		return VM{}, fmt.Errorf("rundir cannot be empty for vm (%s)", vm.Name)
	}

	if err := EnsureDir(runDir); err != nil {
		return VM{}, err
	}

	disks := []QemuDisk{}
	opts := KVMRunOpts{
		Name:           vm.Name,
		Cpus:           2,
		Memory:         4096,
		Boot:           ParseKVMBootSource(vm.Boot),
		Dir:            runDir,
		KVMExtraOpts:   append([]string{"-name", vm.Name}, vm.KVMExtraOpts...),
		ISO:            vm.Cdrom,
		Disks:          disks,
		UefiVars:       vm.UefiVars,
		SecureBoot:     vm.SecureBoot,
		UseTPM:         vm.TPM,
		TPMVersion:     vm.TPMVersion,
		Gui:            vm.Gui,
	}

	if opts.ISO != "" {
		vm.Disks = append(vm.Disks,
			QemuDisk{
				File:   opts.ISO,
				Format: "raw",
				Attach: "ide",
				Type:   "cdrom",
			})
	}
	for _, d := range vm.Disks {
		if err := d.Sanitize(runDir); err != nil {
			return VM{}, err
		}
		// BusAddr only useful for virtio-blk (pci device)
		if d.Attach == "virtio-blk" {
			if d.BusAddr != "" {
				slot, err = strconv.Atoi(d.BusAddr)
				if err != nil {
					return VM{}, errors.Wrapf(err, "Failed to int() BusAddress")
				}
				if err := setPciBusSlot(&opts.PciBusSlots, slot); err != nil {
					return VM{}, errors.Wrap(err, "Failed to setPciBusSlot")
				}
			} else {
				slot := getPciBusSlot(&opts.PciBusSlots)
				d.BusAddr = strconv.Itoa(slot)
			}
		}
		opts.Disks = append(opts.Disks, d)
	}

	return VM{
		Name:               vm.Name,
		endCh:              endCh,
		doneCh:             doneCh,
		opts:               opts,
		State:              VMInit,
	}, nil
}

func (v *VM) Running() bool {
	return v.State == VMStarted
}

func (v *VM) Start() error {
	var err error
	log.Infof("Starting VM:%s", v.Name)

	sockDir, err := v.opts.getSockDir()
	if err != nil {
		return errors.Wrapf(err, "Failed to get sock dir for vm %s", v.Name)
	}
	if err := os.RemoveAll(sockDir); err != nil {
		return errors.Wrapf(err, "Failed purging sockets dir")
	}
	if err := EnsureDir(sockDir); err != nil {
		return errors.Wrapf(err, "Failed creating sockets dir")
	}

	v.swtpmDiedCh = make(chan struct{}, 1)
	// swtpm needs to be started before launching qemu
	if v.opts.UseTPM {
		fpath, err := filepath.Abs(path.Join(v.opts.Dir, "tpm"))
		if err != nil {
			return err
		}
		sock, err := v.opts.TPMSocket()
		if err != nil {
			return err
		}
		v.opts.SwTPM = &SwTPM{
			StateDir: fpath,
			Socket:   sock,
			Version:  v.opts.TPMVersion,
			diedCh:   v.swtpmDiedCh,
		}
		if err := v.opts.SwTPM.Start(); err != nil {
			return errors.Wrapf(err, "Failed to start SwTPM")
		}
		go func() {
			<-v.swtpmDiedCh
			log.Warnf("swtpm died")
			v.Stop()
		}()
	}
	for _, d := range v.opts.Disks {
		if PathExists(d.File) {
			log.Debugf("skipping disk creation, file exists:%s", d.File)
			continue
		}
		if err := d.Create(); err != nil {
			return err
		}
	}

	args, err := getKvmCommand(v.opts)
	if err != nil {
		return errors.Wrapf(err, "Error building qemu command for %v", v)
	}

	outPath := ""
	v.cmd = exec.Command(args[0], args[1:]...)
	v.cmd.Stdin = os.Stdin
	v.cmd.Stdout = os.Stdout
	v.cmd.Stderr = os.Stderr

	log.Infof("Running VM:%s with QemuCmd: %s", v.Name, v.cmd)
	err = v.cmd.Start()
	if err != nil {
		return errors.Wrapf(err, "Error running QemuCmd: %v", args)
	}

	// VMs always have a monitor
	waitOn := []string{}
	if sock, err := v.opts.MonitorSocket(); err == nil {
		waitOn = append(waitOn, sock)
	} else {
		return errors.Wrapf(err, "Failed to get monitor socket")
	}

	v.State = VMStarted

	go func(cmd *exec.Cmd, v *VM) {
		if cmd == nil {
			e := fmt.Errorf("cmd is nil - something went wrong with VM:%s", v.Name)
			log.Warnf("%s", e)
			finalErr = e
			v.doneCh <- struct{}{}
			return
		}
		err := cmd.Wait()
		if err != nil {
			if v.State != VMStopped {
				v.State = VMFailed
				e := fmt.Errorf("VM:%s qemu exited with error: %v", v.Name, err)
				extra := ""
				if outPath != "" {
					if out, err := os.ReadFile(outPath); err == nil {
						extra = fmt.Sprintf("\nqemu-output: %s\n", string(out))
					}
				}
				log.Warnf("%s%s", e, extra)
				finalErr = e
			}
		} else {
			v.State = VMStopped
			log.Infof("VM:%s qemu exited without error", v.Name)
		}
		v.doneCh <- struct{}{}
	}(v.cmd, v)

	for _, sock := range waitOn {
		// wait for 10 seconds for each.
		if !WaitForPath(sock, 10, 1) {
			return fmt.Errorf("VM socket %s does not exist", sock)
		}
	}

	return nil
}

func (v *VM) Stop() error {
	if v.State == VMCleaned {
		return errors.Errorf("Stop called twice on VM:%s", v.Name)
	}

	log.Infof("Stop called for VM:%s", v.Name)
	if v.Running() {
		// TODO use the qemu console to ask it nicely first
		v.State = VMStopped
		err := v.cmd.Process.Kill()
		if err != nil {
			log.Warnf("Error killing VM:%s: %v", v.Name, err)
		}
		// wait until the cmd has been reaped.
		<-v.doneCh
	}

	if v.opts.SwTPM != nil {
		v.opts.SwTPM.Stop()
	}

	sLink := path.Join(v.opts.Dir, "sockets")
	if err := os.Remove(sLink); err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("Failed to remove %s: %v", sLink, err)
		}
	}

	sockDir, err := v.opts.getSockDir()
	if err != nil {
		log.Warnf("getting sockDir failed for vm %s: %v", v.Name, err)
	} else if sockDir != "" {
		if err := os.RemoveAll(sockDir); err != nil {
			log.Warnf("failed to remove socket dir for vm %s: %v", v.Name, err)
		}
	}

	v.State = VMCleaned

	return nil
}

func getPciExpressRootPortsArgs(pcislot int) []string {
	numRootPortsPerSlot := 8 // 8 ports per multifunction pcislot
	args := []string{}
	rootPortTpl := "pcie-root-port,port=0x%x,chassis=0x%d,id=root-port.%d.%d,addr=0x%x.0x%x"

	for port := 0; port < numRootPortsPerSlot; port++ {
		devopts := fmt.Sprintf(rootPortTpl, port, port, pcislot, port, pcislot, port)
		if port == 0 {
			devopts += ",multifunction=on"
		}
		args = append(args, "-device", devopts)
	}

	return args
}

type ovmfPaths struct {
	code string
	vars string
}

func (opts KVMRunOpts) HasExplicitIndices() bool {
	// TODO - handle nics
	for _, d := range opts.Disks {
		if d.BootIndex != "" {
			return true
		}
	}
	return false
}

func (opts *KVMRunOpts) GetBootIndices() map[int]int {
	// determining bootable is a real pain.
	// the logic we want is:
	//  BootCD - none of disks are bootable (only the CD)
	//  BootSSD | BootHDD - mark the first in the list of this type as 0
	//  BootUnspec - mark the first HDD as 1 and the first SSD 0
	bootIndices := map[int]int{}

	if opts.HasExplicitIndices() {
		return bootIndices
	}

	disks := opts.Disks
	boot := opts.Boot

	// No BootIdnex specifed, so figure some out
	firstSSD, firstHDD := true, true

	for n, d := range disks {
		bootIndex := -1

		switch boot {
		case BootCD:
			if d.Type == "cdrom" {
				bootIndex = 0
			}
		case BootSSD:
			if firstSSD && d.Type == "ssd" {
				bootIndex = 0
			}
		case BootHDD:
			if firstHDD && d.Type == "hdd" {
				bootIndex = 0
			}
		case BootUnspec:
			if firstSSD && d.Type == "ssd" {
				bootIndex = 0
			} else if firstHDD && d.Type == "hdd" {
				bootIndex = 1
			}
		}

		if d.Type == "ssd" {
			firstSSD = false
		} else if d.Type == "hdd" {
			firstHDD = false
		}

		bootIndices[n] = bootIndex
	}

	return bootIndices

}

var QemuTypeIndex map[string]int

// Allocate the next number per Qemu Type string
// This is use to create unique, increasing index integers used to
// enumerate qemu id= parameters used to bind various objects together
// on the QEMU command line: e.g
//
// -object iothread,id=iothread2
// -drive id=drv1
// -device scsi-hd,drive=drv1,iothread=iothread2
//
func getNextQemuIndex(qtype string) int {
	currentIndex := 0
	ok := false
	if QemuTypeIndex == nil {
		QemuTypeIndex = make(map[string]int)
	}
	if currentIndex, ok = QemuTypeIndex[qtype]; !ok {
		currentIndex = -1
	}
	QemuTypeIndex[qtype] = currentIndex + 1
	return QemuTypeIndex[qtype]
}

func clearAllQemuIndex() {
	for key := range QemuTypeIndex {
		delete(QemuTypeIndex, key)
	}
}

func diskNeedsBusDevice(attach string) bool {
	switch attach {
	case "usb", "scsi", "ide":
		return true
	}
	return false
}

func (opts *KVMRunOpts) DiskArgs() []string {
	args := []string{}
	busses := []string{}
	pcieBus := "bus=pcie.0"
	index := 0
	ok := false
	indexes := map[string]int{}
	disks := opts.Disks
	bootIndices := opts.GetBootIndices()
	for n, d := range disks {
		if index, ok = indexes[d.Attach]; !ok {
			indexes[d.Attach] = 0
			// create a bus device for attach types which require such
			// allocate an object thread for these as well
			if diskNeedsBusDevice(d.Attach) {
				iothreadID := ""
				device := ""
				switch d.Attach {
				case "usb":
					device = "qemu-xhci"
				case "scsi":
					// enable indirect_descriptors, this increases speed by reducing the
					// number of entries on the vring per IO, reducing vmexits
					device = "virtio-scsi-pci,indirect_desc=on"
				case "ide":
					device = "ich9-ahci"
				}
				devopts := []string{device,
					fmt.Sprintf("id=%s%d", d.Attach, getNextQemuIndex(d.Attach)),
					fmt.Sprintf("addr=%d", getPciBusSlot(&opts.PciBusSlots)),
					pcieBus}
				// if using scsi, enable iothread polling for faster IO submission
				switch d.Attach {
				case "scsi":
					iothreadID = fmt.Sprintf("iothread%d", getNextQemuIndex("iothread"))
					devopts = append(devopts, "iothread="+iothreadID)
					busses = append(busses, "-object", "iothread,poll-max-ns=32,id="+iothreadID)
				}
				busses = append(busses, "-device", strings.Join(devopts, ","))
			}
		}
		indexes[d.Attach] = index + 1
		idx, ok := bootIndices[n]
		if !ok {
			idx = -1
		}
		args = append(args, d.args(index, idx)...)
	}
	return append(busses, args...)
}

func parseOvmfOpts(opts KVMRunOpts) ([]string, error) {
	secCodePath := "/usr/share/OVMF/OVMF_CODE.secboot.fd"
	ubuSecVars := "/usr/share/OVMF/OVMF_VARS.ms.fd"
	centosSecVars := "/usr/share/OVMF/OVMF_VARS.secboot.fd"

	unsecCodePath := "/usr/share/OVMF/OVMF_CODE.fd"
	unsecVarsPath := "/usr/share/OVMF/OVMF_VARS.fd"

	// All OVMF installs provide OVMF_CODE.fd and OVMF_CODE.secboot.fd,
	// if these are not present then OVMF package needs to be installed.
	if !PathExists(unsecCodePath) && !PathExists(secCodePath) {
		fmt.Fprintf(os.Stderr, "Failed to find UEFI OVMF nvram source")
		fmt.Fprintf(os.Stderr, "Please install OVMF package or remove --uefi flag\n")
		return []string{}, errors.Errorf("OVMF package missing")
	}

	var ovmfPath ovmfPaths

	if opts.SecureBoot {
		log.Infof("UEFI: enabling secureboot OVMF config")
		ovmfPath.code = secCodePath
		if PathExists(ubuSecVars) {
			ovmfPath.vars = ubuSecVars
		} else if PathExists(centosSecVars) {
			ovmfPath.vars = centosSecVars
		} else {
			return []string{}, errors.Errorf("secureboot requested, but no secureboot OVMF variables found")
		}
	} else {
		if PathExists(unsecCodePath) { // jenkins runners dont have this one, only secboot.fd
			ovmfPath.code = unsecCodePath
		} else {
			ovmfPath.code = secCodePath
		}
		if PathExists(unsecVarsPath) {
			ovmfPath.vars = unsecVarsPath
		} else {
			return []string{}, errors.Errorf("OMVF variables template missing: %s", unsecVarsPath)
		}
	}

	if opts.UefiVars != "" {
		log.Infof("UEFI: using user provided OVMF variables template: %s", opts.UefiVars)
		ovmfPath.vars = opts.UefiVars
		if !PathExists(ovmfPath.vars) {
			return []string{}, errors.Errorf("specified uefi variables path %s does not exist", ovmfPath.vars)
		}
	}
	log.Infof("UEFI: using OVMF config: %v", ovmfPath)

	ovmfAtomixVars, err := filepath.Abs(path.Join(opts.Dir, "uefi-nvram.fd"))
	if err != nil {
		return []string{}, err
	}

	if PathExists(ovmfAtomixVars) {
		log.Infof("Using existing UEFI NVRAM file: %s\n", ovmfAtomixVars)
	} else {
		if err = EnsureDir(filepath.Dir(ovmfAtomixVars)); err != nil {
			return []string{}, err
		}
		err = CopyFileBits(ovmfPath.vars, ovmfAtomixVars)
		if err != nil {
			return []string{}, errors.Errorf("Failed copying UEFI vars template: %v", err)
		}
		log.Infof("Copied UEFI NVRAM file from %s to %s", ovmfPath.vars, ovmfAtomixVars)
	}
	args := []string{"-drive", "if=pflash,format=raw,readonly=on,file=" + ovmfPath.code}
	args = append(args, "-drive", "if=pflash,format=raw,file="+ovmfAtomixVars)
	return args, nil
}

func getKvmCommand(opts KVMRunOpts) ([]string, error) {
	qCont, err := getQemuKvmContext()
	if err != nil {
		return []string{}, errors.Wrapf(err, "Failed to create QemuKvmContext")
	}
	_, err = qemuVersionOK(&qCont)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Please install newer QEMU/KVM\n")
		fmt.Fprintf(os.Stderr, "For Centos/RHEL: yum install centos-release-qemu-ev; yum update; yum install qemu-kvm-ev\n")
		fmt.Fprintf(os.Stderr, "For Ubuntu, upgrade to Xenial or newer\n")
		return []string{}, errors.Errorf("QEMU/KVM version check failed")
	}

	mem := opts.Memory
	if mem == 0 {
		mem = 4096
	}
	memory := fmt.Sprintf("%d", mem)
	cpus := opts.Cpus
	if opts.Cpus == 0 {
		cpus = 1
	}
	smp := fmt.Sprintf("%d", cpus)
	args := []string{
		qCont.path, "-enable-kvm",
		"-smp", smp, "-m", memory,
		// retain qemu64, add the x2apic flag for enabling ioeventfd notification
		// potential for improvements using -cpu host, however installer relies
		// on cpu name to trigger different paths
		"-cpu", "qemu64,+x2apic",
		// use q35 for UEFI to use OVMF secboot.fd with SMM enabled
		// Note that this does not imply the VM is booting securely as that
		// requires key enrollment through some other process but this does
		// allow the VM to boot via UEFI using the secboot/SMM enabled firmware
		"-M", "q35,smm=on,accel=kvm",
		// Due to the way some of the models work in edk2, we need to disable
		// s3 resume.memory Without this option, qemu will appear to silently hang
		// although it emits an error message on the ovmf_log
		"-global", "ICH9-LPC.disable_s3=1",
		// Mark pflash with Secure Property
		"-global", "driver=cfi.pflash01,property=secure,value=on",
		// Relying on host cpu with flag 'constant_tsc' we do not need an
		// emulated high resolution device, guest will use kvmclock
		"-no-hpet",
		// vitio-scsi bus uses dedicated io thread, enable io polling for faster
		// io submission, especially when running on top of ssd devices
		//"-object", "iothread,id=iothread0,poll-max-ns=32",
		//
	}
	// add some entropy, so we don't have boot stalls due to lack of it
	args = append(args, "-object", "rng-random,filename=/dev/urandom,id=rng0",
		"-device", "virtio-rng-pci,rng=rng0,bus=pcie.0"+fmt.Sprintf(",addr=%d", getPciBusSlot(&opts.PciBusSlots)))

	if opts.Gui {
		spicePort := NextFreePort(5900)
		portStr := fmt.Sprintf("port=%d,disable-ticketing=on", spicePort)
		args = append(args, "-vga", "qxl", "-spice", portStr)
		portPath := filepath.Join(dataDir, "machine", envDir,
			 fmt.Sprintf("%s.rundir", opts.Name), "gui.port")
		err = os.WriteFile(portPath, []byte(fmt.Sprintf("%d", spicePort)), 0600)
		if err != nil {
			return []string{}, err
		}
	} else {
		args = append(args, "-nographic")
	}
	// add a PCI Express Multifunction Root Port devices, 8 slots per PciBusSlot
	args = append(args, getPciExpressRootPortsArgs(getPciBusSlot(&opts.PciBusSlots))...)

	args = append(args, "-nographic")

	s, err := opts.TPMSocket()
	if err != nil {
		return []string{}, err
	}
	args = append(args,
		"-chardev", "socket,id=chrtpm,path="+s,
		"-tpmdev", "emulator,id=tpm0,chardev=chrtpm",
		"-device", "tpm-tis,tpmdev=tpm0")

	ovmfArgs, err := parseOvmfOpts(opts)
	if err != nil {
		return []string{}, err
	}
	args = append(args, ovmfArgs...)

	consolePath, err := opts.ConsoleSocket()
	if err != nil {
		return []string{}, err
	}
	args = append(args, "-serial", fmt.Sprintf("unix:"+consolePath+",server,nowait"))

	sockPath, err := opts.MonitorSocket()
	if err != nil {
		return args, err
	}
	args = append(args, "-monitor", "unix:"+sockPath+",server,nowait")

	// Use hugepages to back guest memory, reduces memory pagetable walking penalty
	hugePath, err := hugetlbfsAvailable()
	if err == nil {
		numHugePages, err := hugetlbfsFreePages()
		if err == nil {
			minNumPages := mem / 2 // 2MB HugePages are much more likely than 1G pages
			if numHugePages >= minNumPages {
				args = append(args, "-mem-path", hugePath)
			} else {
				fmt.Fprintf(os.Stderr, "Not enough hugepages free, found %d expected >= %d\n", numHugePages, minNumPages)
				fmt.Fprintf(os.Stderr, "Try: echo %d | sudo tee /proc/sys/vm/nr_hugepages\n", minNumPages)
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "No hugetlbfs mount points available")
		fmt.Fprintln(os.Stderr, "Try: sudo mount -t hugetlbfs none /dev/hugetlbfs")
	}

	// this allocates the bus controllers which must be on the argument list before an device/drive references it
	args = append(args, opts.DiskArgs()...)

	for _, o := range opts.KVMExtraOpts {
		args = append(args, strings.Split(o, " ")...)
	}

	args = append(args, "-net", "none")

	switch opts.Boot {
	case BootNet:
		args = append(args, "-boot", "n")
	}

	log.Debugf("KVM Args: %s", args)
	return args, nil
}
