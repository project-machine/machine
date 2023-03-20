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
	"fmt"
	"path"
	"path/filepath"
	"strings"

	humanize "github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
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
	File      string   `yaml:"file,omitempty"`
	Format    string   `yaml:"format,omitempty"`
	Size      DiskSize `yaml:"size"`
	Attach    string   `yaml:"attach,omitempty"`
	Type      string   `yaml:"type"`
	BlockSize int      `yaml:"blocksize,omitempty"`
	BusAddr   string   `yaml:"addr,omitempty"`
	BootIndex string   `yaml:"bootindex,omitempty"`
	ReadOnly  bool     `yaml:"read-only,omitempty"`
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
