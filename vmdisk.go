package main

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/apex/log"
	humanize "github.com/dustin/go-humanize"
)

type DiskSize int64

func (s *DiskSize) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var strVal string
	if err := unmarshal(&strVal); err != nil {
		return err
	}
	ut, err := humanize.ParseBytes(strVal)
	if err != nil {
		return err
	}

	*s = DiskSize(ut)
	return nil
}

type QemuDisk struct {
	File      string   `yaml:"file"`
	Format    string   `yaml:"format"`
	Size      DiskSize `yaml:"size"`
	Attach    string   `yaml:"attach"`
	Type      string   `yaml:"type"`
	BlockSize int      `yaml:"blocksize"`
	BusAddr   string   `yaml:"addr"`
	BootIndex string   `yaml:"bootindex"`
	ReadOnly  bool     `yaml:"read-only"`
}

func (q *QemuDisk) Sanitize(basedir string) error {
	validate := func(name string, found string, valid ...string) string {
		for _, i := range valid {
			if found == i {
				return ""
			}
		}
		return fmt.Sprintf("invalid %s: found %s expected %v", name, found, valid)
	}

	errors := []string{}

	if q.Format == "" {
		q.Format = "qcow2"
	}

	if q.Type == "" {
		q.Type = "ssd"
	}

	if q.Attach == "" {
		q.Attach = "scsi"
	}

	if q.File == "" {
		errors = append(errors, "empty File")
	}

	if !strings.Contains(q.File, "/") {
		q.File = path.Join(basedir, q.File)
	}

	if msg := validate("format", q.Format, "qcow2", "raw"); msg != "" {
		errors = append(errors, msg)
	}

	if msg := validate("attach", q.Attach, "scsi", "nvme", "virtio", "ide", "usb"); msg != "" {
		errors = append(errors, msg)
	}

	if msg := validate("type", q.Type, "hdd", "ssd", "cdrom"); msg != "" {
		errors = append(errors, msg)
	}

	if len(errors) != 0 {
		return fmt.Errorf("bad disk %#v: %s", q, strings.Join(errors, "\n"))
	}

	return nil
}

// Create - create the qemu disk at fpath or its File if it does not exist.
func (q *QemuDisk) Create() error {
	if q.Type == "cdrom" {
		log.Debugf("Ignoring Create on QemuDisk.Name:%s wth Type 'cdrom'", q.File)
		return nil
	} else if q.Size == 0 {
		log.Debugf("Ignoring Create on QemuDisk.Name:%s with Size '0'", q.File)
		return nil
	}
	log.Infof("Creating %s type %s size %d attach %s", q.File, q.Format, q.Size, q.Attach)
	cmd := []string{"qemu-img", "create", "-f", q.Format, q.File, fmt.Sprintf("%d", q.Size)}
	out, err, rc := RunCommandWithOutputErrorRc(cmd...)
	if rc != 0 {
		return fmt.Errorf("qemu-img create failed: %v\n rc: %d\n out: %s\n, err: %s",
			cmd, rc, out, err)
	}
	return nil
}

func (q *QemuDisk) serial() string {
	// serial gets basename without extension
	ext := filepath.Ext(q.File)
	s := path.Base(q.File[0 : len(q.File)-len(ext)])
	if q.Type == "ssd" && q.Attach == "virtio" && !strings.HasPrefix("ssd-", s) {
		// virtio-blk does not support rotation_rate. Some places (partition-helpers and disko)
		// determine that a disk is an ssd if it's serial starts with 'ssd-'
		s = "ssd-" + s
	}
	return s
}

// args - return a list of strings to pass to qemu
func (q *QemuDisk) args(attachIndex int, bootIndex int) []string {
	driver := ""
	ssd := "ssd"
	driveID := fmt.Sprintf("drive%d", getNextQemuIndex("drive"))

	switch q.Attach {
	case "virtio":
		driver = "virtio-blk"
	case "ide":
		driver = "ide-hd"
		if q.Type == "cdrom" {
			driver = "ide-cd"
		}
	case "usb":
		driver = "usb-storage"
	case "scsi":
		driver = "scsi-hd"
		if q.Type == "cdrom" {
			driver = "scsi-cd"
		}
	default:
		driver = q.Attach
	}

	driveopts := []string{
		"file=" + q.File,
		"id=" + driveID,
		"if=none",
		"format=" + q.Format,
		"aio=threads",  // use host threadpool for submitting/completing IO
		"cache=unsafe", // allows guest writes to return after submitting to qemu
		// significantly speeds up IO at the cost of data loss
		// if QEMU crashes
		"discard=unmap",       // unmap block in disk when guest issues trim/discard
		"detect-zeroes=unmap", // unmap block in disk when guest issues writes of zeroes
	}

	devopts := []string{
		driver,
		"drive=" + driveID,
		"serial=" + q.serial(),
	}

	if q.ReadOnly || q.Type == "cdrom" {
		driveopts = append(driveopts, "readonly=on")
	}

	if q.Type == "cdrom" {
		if q.Attach == "virtio" {
			driveopts = append(driveopts, "media=cdrom")
		}
	}

	if q.Attach == "virtio" {
		// only virtio-pci devices can use .addr property
		if q.BusAddr != "" {
			devopts = append(devopts, fmt.Sprintf("addr=%s", q.BusAddr))
		}
		devopts = append(devopts, "bus=pcie.0")
	}

	if q.BootIndex != "" {
		devopts = append(devopts, "bootindex="+q.BootIndex)
	} else if bootIndex >= 0 {
		devopts = append(devopts, fmt.Sprintf("bootindex=%d", bootIndex))
	}

	// Set a reasonable default rotation_rate for disks by Attach/Type
	if q.Attach == "scsi" || q.Attach == "ide" {
		rate := ""
		if q.Type == ssd {
			rate = "1" // Indicates to Linux this is an SSD
		} else if q.Type == "hdd" {
			if q.Attach == "scsi" {
				rate = "15000"
			} else if q.Attach == "ide" {
				rate = "7200"
			}
		}
		if rate != "" {
			devopts = append(devopts, fmt.Sprintf("rotation_rate="+rate))
		}
	}

	// sort out what bus to use
	if q.Attach == "ide" {
		// ich9-ahci is a SATA controller with 6 ports
		if attachIndex > 6 {
			log.Fatalf("Can't have more than 6 ide disks. (found %d)", attachIndex)
		}
		devopts = append(devopts, fmt.Sprintf("bus=ide.%d", attachIndex))
	}

	if q.BlockSize != 0 {
		devopts = append(devopts,
			fmt.Sprintf("logical_block_size=%d", q.BlockSize),
			fmt.Sprintf("physical_block_size=%d", q.BlockSize))
	}

	return []string{
		"-drive", strings.Join(driveopts, ","),
		"-device", strings.Join(devopts, ","),
	}
}
