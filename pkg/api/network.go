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
	"math/rand"
	"time"
)

type NetworkDef struct {
	Name    string `yaml:"name"`
	Address string `yaml:"address",omitempty`
	Type    string `yaml:"type"`
	IFName  string `yaml:"interface",omitempty`
}

type NicDef struct {
	BusAddr   string     `yaml:"addr,omitempty"`
	Device    string     `yaml:"device"`
	ID        string     `yaml:"id",omitempty`
	Mac       string     `yaml:"mac",omitempty`
	ifname    string     `yaml:"ifname",omitempty`
	Network   string     `yaml:"network",omitempty`
	Ports     []PortRule `yaml:"ports",omitempty`
	BootIndex string     `yaml:"bootindex,omitempty`
	ROMFile   string     `yaml:"romfile,omitempty`
}

type VMNic struct {
	BusAddr    string
	DeviceType string
	HWAddr     string
	ID         string
	IFName     string
	NetIFName  string
	NetType    string
	NetAddr    string
	BootIndex  string
	Ports      []PortRule
}

// Ports are a list of PortRules
// nics:
//  - id: nic1
//    ports:
//    - protocol: tcp
//		host:
//        address: ""  // address must be an IP, not hostname
//        port: 22222
//		guest:
//        address: ""
//        port: 22
//    - host:
//        address: ""
//        port: 1234
//    - guest:
//        address: ""
//        port: 23
//    - host:
//        address: ""
//        port: 8080
//    - guest:
//        address: ""
//        port: 80

// A PortRule is a single entry map where the key and value represent
// the host and guest mapping respectively. The Host and Guest value

type PortRule struct {
	Protocol string
	Host     Port
	Guest    Port
}

type Port struct {
	Address string
	Port    int
}

func (p *PortRule) String() string {
	return fmt.Sprintf("%s:%s:%d-%s:%d", p.Protocol,
		p.Host.Address, p.Host.Port, p.Guest.Address, p.Guest.Port)
}

// https://stackoverflow.com/questions/21018729/generate-mac-address-in-go
func RandomMAC() (string, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := make([]byte, 6)
	_, err := r.Read(buf)
	if err != nil {
		return "", fmt.Errorf("Failed reading random bytes")
	}

	// Set local bit, ensure unicast address
	buf[0] = (buf[0] | 2) & 0xfe
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5]), nil
}

func RandomQemuMAC() (string, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	buf := make([]byte, 6)
	suf := make([]byte, 3)
	_, err := r.Read(suf)
	if err != nil {
		return "", fmt.Errorf("Failed reading random bytes")
	}
	// QEMU OUI prefix 52:54:00
	buf[0] = 0x52
	buf[1] = 0x54
	buf[2] = 0x00
	buf[3] = suf[0]
	buf[4] = suf[1]
	buf[5] = suf[2]
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5]), nil
}
