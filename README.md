# Machine

## Introduction

Machine is a tool(set) to create, install, and run your container
images in a secure manner.

## Status

Currently all machine does is run kvm vms.  However, it does so
easily, driven by yaml specs, using secureboot and a UEFI db
configured by yourself.

## Install Prerequisites

```
sudo add-apt-repository -y ppa:puzzleos/dev
sudo apt install golang-go || sudo snap install --classic go
sudo apt install -y build-essential qemu-system-x86 qemu-utils spice-client-gtk socat swtpm
sudo usermod --append --groups kvm $USER
newgrp kvm  # or logout and login, run 'groups' command to confirm
```

## Build machine

Find the latest release here: https://github.com/project-machine/machine/releases/latest
And select the tar.gz link, for example:

```
LATEST="https://github.com/project-machine/machine/archive/refs/tags/v0.0.2.tar.gz"
wget "$LATEST"
tar xzf v0.0.2
cd machine-0.0.2
make
```

## Run machined

### Debugging/Testing

In a second shell/terminal

```
newgrp kvm
./bin/machined
```

When done, control-c to stop daemon.

### For hosting/running

In a second shell/terminal

```
groups | grep kvm || newgrp kvm
systemd-run --user --unit=machined.service --no-block --working-directory=$PWD bin/machined
systemctl --user status machined.service
journalctl --user --follow -u machined.service
```


When done, `systemctl stop --user machined.service` The service unit should
be removed from the system.  Run the `systemd-run` command to start it up
again.  If machined fails, you can clean up the unit with `systemctl --user reset-failed machined.service`
then issue the stop command again to remove the unit.

Note: on some systems, systemd-run --user prevents access to /dev/kvm via groups
The current workaround is to `sudo chmod 0666 /dev/kvm`

## Run machine client

```
/bin machine list
```

## Starting your first VM

Download a live iso, like Ubuntu 22.04

https://releases.ubuntu.com/22.04.2/ubuntu-22.04.2-desktop-amd64.iso

```
$ cat >vm1.yaml <<EOF
name: vm1
type: kvm
ephemeral: false
description: A fresh VM booting Ubuntu LiveCD in SecureBoot mode with TPM
config:
  name: vm1
  boot: cdrom
  uefi: true
  tpm: true
  tpm-version: 2.0
  secure-boot: true
  cdrom: ubuntu-22.04.2-desktop-amd64.iso
  disks:
      - file: root-disk.qcow
        type: ssd
        size: 50GiB
EOF
$ ./bin/machine init <vm1.yaml
2023/03/06 22:27:10  info DoCreateMachine Name:rational-pig File:- Edit:false
2023/03/06 22:27:10  info Creating machine...
Got config:
name: vm1
type: kvm
ephemeral: false
description: A fresh VM booting Ubuntu LiveCD in SecureBoot mode with TPM
config:
  name: vm1
  boot: cdrom
  uefi: true
  tpm: true
  tpm-version: 2.0
  secure-boot: true
  cdrom: ubuntu-22.04.2-desktop-amd64.iso
  disks:
      - file: root-disk.qcow
        type: ssd
        size: 50GiB
 200 OK
```

Then start and connect to the console or gui

```
$ bin/machine start vm1
200 OK
$ bin/machine gui vm1
```
