package main

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/apex/log"
	"github.com/pkg/errors"
)

const randChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(length int) string {
	if length == 0 {
		length = 8
	}
	randCharLength := len(randChars)
	b := make([]byte, length)
	for i := range b {
		b[i] = randChars[rand.Intn(randCharLength)]
	}
	return string(b)
}

const LinuxIfnameLength int = 15

// generate a random Linux network interface name.
// Allow specifying a prefix
func randomNicIFName(prefix string) string {
	prefixLength := len(prefix)
	if prefixLength >= LinuxIfnameLength {
		return randomString(LinuxIfnameLength)
	}
	randLength := LinuxIfnameLength - prefixLength
	return prefix + randomString(randLength)
}

// The VMNic lifecycle: first you create one with vm.AddNic().
// The you call Setup on it to create the tap.  Then you call
// Args to get the qemu commandline to use it.  Finally you
// call Cleanup to tear down the nic.
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

func (v *VM) AddNic(nicdef *NicDef, netdef *NetworkDef) error {
	log.Debugf("AddNic: Nic: %v on Network %v", nicdef, netdef)
	nicdef.IFName = randomNicIFName("anic")
	nic := VMNic{
		BusAddr:    nicdef.BusAddr,
		DeviceType: nicdef.Device,
		HWAddr:     nicdef.Mac,
		ID:         nicdef.ID,
		IFName:     nicdef.IFName,
		NetIFName:  netdef.IFName,
		NetType:    netdef.Type,
		NetAddr:    netdef.Address,
		BootIndex:  nicdef.BootIndex,
		Ports:      nicdef.Ports,
	}
	log.Debugf("AddNic: Nic:%s Slot:%s -> IFName:%s Type:%s", nic.ID, nic.BusAddr, nic.IFName, nic.NetType)
	v.opts.NICs = append(v.opts.NICs, &nic)
	return nil
}

var macOffset int = 0

func (n *VMNic) Cleanup() {
	if n.NetType == "user" || n.NetType == "mcast" {
		return
	}
	cmd := []string{"ip", "link", "set", n.IFName, "down"}
	err := RunCommand(cmd...)
	if err != nil {
		log.Infof("Command %v failed: %v", cmd, err)
	}
	cmd = []string{"ip", "link", "set", n.IFName, "nomaster"}
	err = RunCommand(cmd...)
	if err != nil {
		log.Infof("Command %v failed: %v", cmd, err)
	}
	cmd = []string{"ip", "link", "del", n.IFName}
	err = RunCommand(cmd...)
	if err != nil {
		log.Infof("Command %v failed: %v", cmd, err)
	}
}

func newMacAddr() string {
	d := 2 + macOffset
	macOffset++
	return fmt.Sprintf("52:54:00:a2:34:%02x", d)
}

func (n *VMNic) Setup() error {
	log.Debugf("Setting up Nic:%s Type:%s IFName:%s", n.ID, n.NetType, n.IFName)
	if n.NetType == "user" {
		return nil
	}
	if n.NetType == "bridge" {
		// TODO: for non-root VMs, need to set user and group on tuntap
		cmd := []string{"ip", "tuntap", "add", n.IFName, "mode", "tap"}
		err := RunCommand(cmd...)
		if err != nil {
			return errors.Wrapf(err, "Error running '%s'", cmd)
		}
		cmd = []string{"ip", "link", "set", n.IFName, "up"}
		err = RunCommand(cmd...)
		if err != nil {
			return errors.Wrapf(err, "Error running '%s'", cmd)
		}
		cmd = []string{"ip", "link", "set", n.IFName, "master", n.NetIFName}
		err = RunCommand(cmd...)
		if err != nil {
			return errors.Wrapf(err, "Error running '%s'", cmd)
		}
	}
	if n.HWAddr == "" {
		n.HWAddr = newMacAddr()
	}
	log.Debugf("NIC %s on %s %s has macaddr %s", n.IFName, n.NetType, n.NetIFName, n.HWAddr)
	return nil
}

func (n *VMNic) Args() []string {
	// -netdev tap|user,id=$Name,opts|socket,mcast=
	netdev := []string{}
	// -device $device,mac=$HWAaddr,addr=$Addr,netdev=$Name
	device := []string{}

	switch n.NetType {
	case "user":
		netdev = append(netdev, "user")
		if n.NetAddr != "" {
			netdev = append(netdev, "net="+n.NetAddr)
		}
	case "mcast":
		netdev = append(netdev, "socket")
		if n.NetAddr != "" {
			netdev = append(netdev, "mcast="+n.NetAddr)
		}
	default:
		netdev = append(netdev, "tap", "ifname="+n.IFName, "script=no", "downscript=no")
	}
	netdev = append(netdev, "id="+n.ID)
	if len(n.Ports) > 0 {
		for idx := range n.Ports {
			netdev = append(netdev, "hostfwd="+n.Ports[idx].String())
		}
	}

	if n.DeviceType == "" {
		device = append(device, "virtio-net")
	} else {
		device = append(device, n.DeviceType)
	}
	if n.BusAddr != "" {
		device = append(device, "addr="+n.BusAddr)
	}
	if n.HWAddr != "" {
		device = append(device, "mac="+n.HWAddr)
	}
	device = append(device, "netdev="+n.ID)

	if n.BootIndex != "" {
		device = append(device, "bootindex="+n.BootIndex)
	}

	// use the default pcie.0 bus
	device = append(device, "bus=pcie.0")

	return []string{
		"-device", strings.Join(device, ","),
		"-netdev", strings.Join(netdev, ",")}
}
