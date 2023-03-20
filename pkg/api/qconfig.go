package api

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/raharper/qcli"

	log "github.com/sirupsen/logrus"
)

func GetKvmPath() (string, error) {
	// perfer qemu-kvm, qemu-system-x86_64, kvm
	emulators := []string{"qemu-kvm", "qemu-system-x86_64", "kvm"}
	paths := []string{"/usr/libexec", "/usr/bin"}

	for _, emulator := range emulators {
		for _, prefix := range paths {
			kvmPath := path.Join(prefix, emulator)
			if _, err := os.Stat(kvmPath); err == nil {
				return kvmPath, nil
			}
		}
	}
	return "", fmt.Errorf("Failed to find QEMU/KVM binary [%s] in paths [%s]\n", emulators, paths)
}

func NewDefaultConfig(name string, numCpus, numMemMB uint32, sockDir string) (*qcli.Config, error) {
	smp := qcli.SMP{CPUs: numCpus}
	if numCpus < 1 {
		smp.CPUs = 4
	}

	mem := qcli.Memory{
		Size: fmt.Sprintf("%dm", numMemMB),
	}
	if numMemMB < 1 {
		mem.Size = "4096m"
	}

	path, err := GetKvmPath()
	if err != nil {
		return &qcli.Config{}, fmt.Errorf("Failed creating new default config: %s", err)
	}

	c := &qcli.Config{
		Name: name,
		Path: path,
		Machine: qcli.Machine{
			Type:         qcli.MachineTypePC35,
			Acceleration: qcli.MachineAccelerationKVM,
			SMM:          "on",
		},
		CPUModel:      "qemu64",
		CPUModelFlags: []string{"+x2apic"},
		SMP:           smp,
		Memory:        mem,
		RngDevices: []qcli.RngDevice{
			qcli.RngDevice{
				Driver:    qcli.VirtioRng,
				ID:        "rng0",
				Bus:       "pcie.0",
				Transport: qcli.TransportPCI,
				Filename:  qcli.RngDevUrandom,
			},
		},
		CharDevices: []qcli.CharDevice{
			qcli.CharDevice{
				Driver:  qcli.LegacySerial,
				Backend: qcli.Socket,
				ID:      "serial0",
				Path:    filepath.Join(sockDir, "console.sock"),
			},
			qcli.CharDevice{
				Driver:  qcli.LegacySerial,
				Backend: qcli.Socket,
				ID:      "monitor0",
				Path:    filepath.Join(sockDir, "monitor.sock"),
			},
		},
		LegacySerialDevices: []qcli.LegacySerialDevice{
			qcli.LegacySerialDevice{
				ChardevID: "serial0",
			},
		},
		MonitorDevices: []qcli.MonitorDevice{
			qcli.MonitorDevice{
				ChardevID: "monitor0",
			},
		},
		QMPSockets: []qcli.QMPSocket{
			qcli.QMPSocket{
				Type:   "unix",
				Server: true,
				NoWait: true,
				Name:   filepath.Join(sockDir, "qmp.sock"),
			},
		},
		PCIeRootPortDevices: []qcli.PCIeRootPortDevice{
			qcli.PCIeRootPortDevice{
				ID:            "root-port.0x4.0",
				Bus:           "pcie.0",
				Chassis:       "0x0",
				Slot:          "0x00",
				Port:          "0x0",
				Addr:          "0x5",
				Multifunction: true,
			},
			qcli.PCIeRootPortDevice{
				ID:            "root-port.0x4.1",
				Bus:           "pcie.0",
				Chassis:       "0x1",
				Slot:          "0x00",
				Port:          "0x1",
				Addr:          "0x5.0x1",
				Multifunction: false,
			},
		},
		VGA: "qxl",
		SpiceDevice: qcli.SpiceDevice{
			HostAddress:      "127.0.0.1",
			Port:             fmt.Sprintf("%d", NextFreePort(qcli.RemoteDisplayPortBase)),
			DisableTicketing: true,
		},
		GlobalParams: []string{
			"ICH9-LPC.disable_s3=1",
			"driver=cfi.pflash01,property=secure,value=on",
		},
		Knobs: qcli.Knobs{
			NoHPET:    true,
			NoGraphic: true,
		},
	}

	return c, nil
}

// FIXME: what to do with remote client/server ? push to zot and use zot URLs?
// ImportDiskImage will copy/create a source image to server image
func (qd *QemuDisk) ImportDiskImage(imageDir string) error {
	// What to do about sparse? use reflink and sparse=auto for now.
	if qd.Size > 0 {
		if PathExists(qd.File) {
			log.Infof("Skipping creation of existing disk: %s", qd.File)
			return nil
		}
		return qd.Create()
	}

	if !PathExists(qd.File) {
		return fmt.Errorf("Disk File %q does not exist", qd.File)
	}

	if qd.Type == "cdrom" {
		log.Infof("Skipping import of cdrom: %s", qd.File)
	}

	srcFilePath := qd.File
	destFilePath := filepath.Join(imageDir, filepath.Base(srcFilePath))
	qd.File = destFilePath

	if srcFilePath != destFilePath || !PathExists(destFilePath) {
		log.Infof("Importing VM disk '%s' -> '%s'", srcFilePath, destFilePath)
		err := CopyFileRefSparse(srcFilePath, destFilePath)
		if err != nil {
			return fmt.Errorf("Error copying VM disk '%s' -> '%s': %s", srcFilePath, destFilePath, err)
		}
	} else {
		log.Infof("VM disk imported %q", filepath.Base(srcFilePath))
	}

	return nil
}

func (qd *QemuDisk) QBlockDevice(qti *qcli.QemuTypeIndex) (qcli.BlockDevice, error) {
	log.Debugf("QemuDisk -> QBlockDevice() %+v", qd)
	blk := qcli.BlockDevice{
		ID:           fmt.Sprintf("drive%d", qti.NextDriveIndex()),
		File:         qd.File,
		Interface:    qcli.NoInterface,
		AIO:          qcli.Threads,
		BusAddr:      qd.BusAddr,
		ReadOnly:     qd.ReadOnly,
		Cache:        qcli.CacheModeUnsafe,
		Discard:      qcli.DiscardUnmap,
		DetectZeroes: qcli.DetectZeroesUnmap,
		Serial:       qd.serial(),
	}
	if blk.BlockSize == 0 {
		blk.BlockSize = 512
	}
	if qd.BootIndex != "" {
		bootindex, err := strconv.Atoi(qd.BootIndex)
		if err != nil {
			return blk, fmt.Errorf("Failed parsing disk %s BootIndex '%s': %s", qd.File, qd.BootIndex, err)
		}
		blk.BootIndex = fmt.Sprintf("%d", bootindex)
	}

	if qd.Format != "" {
		switch qd.Format {
		case "raw":
			blk.Format = qcli.RAW
		case "qcow2":
			blk.Format = qcli.QCOW2
		}
	} else {
		blk.Format = qcli.QCOW2
	}

	if qd.Attach == "" {
		qd.Attach = "virtio"
	}

	switch qd.Attach {
	case "scsi":
		blk.Driver = qcli.SCSIHD
		blk.SCSI = true
		// FIXME: we should scan disks for buses, create buses, then
		// walk disks a second time to configure bus= for each device
		blk.Bus = "scsi0.0" // this is the default scsi bus
	case "nvme":
		blk.Driver = qcli.NVME
	case "virtio":
		blk.Driver = qcli.VirtioBlock
		blk.Bus = "pcie.0"
		if qd.Type == "cdrom" {
			blk.Media = "cdrom"
		}
	case "ide":
		if qd.Type == "cdrom" {
			blk.Driver = qcli.IDECDROM
			blk.Media = "cdrom"
		} else {
			blk.Driver = qcli.IDEHardDisk
		}
		blk.Bus = "ide.0"
	case "usb":
		blk.Driver = qcli.USBStorage
	default:
		return blk, fmt.Errorf("Unknown Disk Attach type: %s", qd.Attach)
	}

	return blk, nil
}

func (nd NicDef) QNetDevice(qti *qcli.QemuTypeIndex) (qcli.NetDevice, error) {
	//FIXME: how do we do bridge or socket/mcast types?
	ndev := qcli.NetDevice{
		Type:       qcli.USER,
		ID:         fmt.Sprintf("net%d", qti.NextNetIndex()),
		Addr:       nd.BusAddr,
		MACAddress: nd.Mac,
		User: qcli.NetDeviceUser{
			IPV4: true,
		},
		Driver: qcli.DeviceDriver(nd.Device),
	}
	if ndev.MACAddress == "" {
		mac, err := RandomQemuMAC()
		if err != nil {
			return qcli.NetDevice{}, fmt.Errorf("Failed to generate a random QEMU mac: %s", err)
		}
		ndev.MACAddress = mac
	}
	if nd.BootIndex != "" {
		bootindex, err := strconv.Atoi(nd.BootIndex)
		if err != nil {
			return qcli.NetDevice{}, fmt.Errorf("Failed parsing nic %s BootIndex '%s': %s", nd.Device, nd.BootIndex, err)
		}
		ndev.BootIndex = fmt.Sprintf("%d", bootindex)
	}

	return ndev, nil
}

func ConfigureUEFIVars(c *qcli.Config, srcVars, runDir string, secureBoot bool) error {
	uefiDev, err := qcli.NewSystemUEFIFirmwareDevice(secureBoot)
	if err != nil {
		return fmt.Errorf("failed to create a UEFI Firmware Device: %s", err)
	}

	src := uefiDev.Vars
	if len(srcVars) > 0 && PathExists(srcVars) {
		src = srcVars
	}

	dest := filepath.Join(runDir, qcli.UEFIVarsFileName)
	if !PathExists(dest) {
		if err := CopyFileBits(src, dest); err != nil {
			return fmt.Errorf("Failed to copy UEFI Vars from '%s' to '%q': %s", src, dest, err)
		}
	}
	uefiDev.Vars = dest
	c.UEFIFirmwareDevices = []qcli.UEFIFirmwareDevice{*uefiDev}
	return nil
}

func GenerateQConfig(runDir, sockDir string, v VMDef) (*qcli.Config, error) {
	c, err := NewDefaultConfig(v.Name, v.Cpus, v.Memory, sockDir)
	if err != nil {
		return c, err
	}

	err = ConfigureUEFIVars(c, v.UEFIVars, runDir, v.SecureBoot)
	if err != nil {
		return c, fmt.Errorf("Error configuring UEFI Vars: %s", err)
	}

	cdromPath := v.Cdrom
	if !strings.HasPrefix(v.Cdrom, "/") {
		cwd, err := os.Getwd()
		if err != nil {
			return c, fmt.Errorf("Failed to get current working dir: %s", err)
		}
		cdromPath = filepath.Join(cwd, v.Cdrom)
	}

	qti := qcli.NewQemuTypeIndex()

	if v.Cdrom != "" {
		qd := QemuDisk{
			File:     cdromPath,
			Format:   "raw",
			Attach:   "ide",
			Type:     "cdrom",
			ReadOnly: true,
		}
		if v.Boot == "cdrom" {
			qd.BootIndex = "0"
			log.Infof("Boot from cdrom requested: bootindex=%s", qd.BootIndex)
		}
		v.Disks = append(v.Disks, qd)
	}

	if err := v.AdjustBootIndicies(qti); err != nil {
		return c, err
	}

	busses := make(map[string]bool)
	for i := range v.Disks {
		var disk *QemuDisk
		disk = &v.Disks[i]

		if err := disk.Sanitize(runDir); err != nil {
			return c, err
		}

		// import/create files into stateDir/images/basename(File)
		if err := disk.ImportDiskImage(runDir); err != nil {
			return c, err
		}

		qblk, err := disk.QBlockDevice(qti)
		if err != nil {
			return c, err
		}
		c.BlkDevices = append(c.BlkDevices, qblk)

		_, ok := busses[disk.Attach]
		// we only need one controller per attach
		if !ok {
			if disk.Attach == "scsi" {
				scsiCon := qcli.SCSIControllerDevice{
					ID:       fmt.Sprintf("scsi%d", qti.Next("scsi")),
					IOThread: fmt.Sprintf("iothread%d", qti.Next("iothread")),
				}
				c.SCSIControllerDevices = append(c.SCSIControllerDevices, scsiCon)
			}
			if disk.Attach == "ide" {
				ideCon := qcli.IDEControllerDevice{
					Driver: qcli.ICH9AHCIController,
					ID:     fmt.Sprintf("ide%d", qti.Next("ide")),
				}
				c.IDEControllerDevices = append(c.IDEControllerDevices, ideCon)
			}
		}
	}

	for _, nic := range v.Nics {
		qnet, err := nic.QNetDevice(qti)
		if err != nil {
			return c, err
		}
		c.NetDevices = append(c.NetDevices, qnet)
	}

	if v.TPM {
		c.TPM = qcli.TPMDevice{
			ID:     "tpm0",
			Driver: qcli.TPMTISDevice,
			Path:   filepath.Join(runDir, "tpm0.sock"),
			Type:   qcli.TPMEmulatorDevice,
		}
	}

	return c, nil
}

type QMPMachineLogger struct{}

func (l QMPMachineLogger) V(level int32) bool {
	return true
}

func (l QMPMachineLogger) Infof(format string, v ...interface{}) {
	log.Infof(format, v...)
}

func (l QMPMachineLogger) Warningf(format string, v ...interface{}) {
	log.Warnf(format, v...)
}

func (l QMPMachineLogger) Errorf(format string, v ...interface{}) {
	log.Errorf(format, v...)
}
