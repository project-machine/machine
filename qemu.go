package main

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

)

type QemuKvmContext struct {
	path  string
	major int
	minor int
	micro int
}

func getKvmPath() (string, error) {
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

func getQemuVersion(kvmPath string) (major int, minor int, micro int, err error) {
	cmd := []string{kvmPath, "-version"}
	out, rv := RunCommandWithRc(cmd...)
	if rv != 0 {
		return major, minor, micro, fmt.Errorf("Failed to run command: %s", cmd)
	}

	x := string(out)
	// find start of the version in something like
	// QEMU emulator version 2.12.0 (qemu-kvm-ev-2.12.0-44.1.el7_8.1)
	for len(x) > 0 && (x[0] < '0' || x[0] > '9') {
		x = x[1:]
	}
	// End of String, error
	if len(x) == 0 {
		return major, minor, micro, fmt.Errorf("Failed to parse QemuVersion: %s", out)
	}
	// Extract major.minor.micro
	n, err := fmt.Sscanf(x, "%d.%d.%d", &major, &minor, &micro)
	if err != nil || n != 3 {
		return major, minor, micro, fmt.Errorf("Failed to parse QemuVersion: %s", out)
	}
	return
}

func getQemuKvmContext() (QemuKvmContext, error) {
	qContext := QemuKvmContext{}
	kvmPath, err := getKvmPath()
	if err != nil {
		return qContext, fmt.Errorf("Cannot create QemuKvmContext, no path to binary")
	}
	major, minor, micro, err := getQemuVersion(kvmPath)
	if err != nil {
		return qContext, fmt.Errorf("Error getting QEMU/KVM Version from %s", kvmPath)
	}
	qContext.path = kvmPath
	qContext.major = major
	qContext.minor = minor
	qContext.micro = micro
	return qContext, nil
}

func qemuVersionOK(ctx *QemuKvmContext) (bool, error) {
	// QEMU/KVM 2.12.0 is baseline for Centos8/Centos7+qemu-kvm-ev)
	// QEMU/KVM 2.5.0 is baseline for Ubuntu (Xenial or newer)
	// 2.0.0 is not acceptable (Centos7 + epel qemu-system-x86)
	// 1.5.7 is not acceptable (Centos7 + qemu-kvm)
	if ctx.major < 2 || (ctx.major == 2 && ctx.minor < 5) {
		return false, fmt.Errorf("QEMU/KVM Version %d.%d.%d below required 2.5.0", ctx.major, ctx.minor, ctx.micro)
	}
	return true, nil
}

func hugetlbfsAvailable() (string, error) {

	content, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", fmt.Errorf("Error reading /proc/mounts")
	}
	mountData := strings.Split(string(content), "\n")
	for _, entry := range mountData {
		fields := strings.Fields(entry)
		if len(fields) > 0 {
			if fields[0] == "hugetlbfs" {
				return fields[1], nil
			}
		}
	}
	return "", fmt.Errorf("Did not find any hugetlbfs mount entries")
}

func hugetlbfsFreePages() (int, error) {
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("Error reading /proc/meminfo")
	}
	memData := strings.Split(string(content), "\n")
	for _, entry := range memData {
		fields := strings.Fields(entry)
		if fields[0] == "HugePages_Free:" {
			numFree, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, fmt.Errorf("Failed to parse %s", fields[1])
			}
			return numFree, nil
		}
	}
	return 0, fmt.Errorf("Did not find HugePages_Free entry in /proc/meminfo")
}

