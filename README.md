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

```shell
LATEST="https://github.com/project-machine/machine/archive/refs/tags/v0.0.4.tar.gz"
wget "$LATEST"
tar xzf v0.0.4
cd machine-0.0.4
make
```

## Run machined

### Debugging/Testing

In a second shell/terminal

```shell
newgrp kvm
./bin/machined
```

When done, control-c to stop daemon.

### For hosting/running

In a second shell/terminal, use `machined install` to setup systemd units to run
machined via socket activation.

```shell
groups | grep kvm || newgrp kvm
./bin/machined install
systemctl --user status machined.service
journalctl --user --follow -u machined.service
```

If you make changes to machined (most changes under pkg/api) then you can stop
the service with `systemctl stop --user machined.service` and then any new
invocation of `machine` will start up the service again with the newer binary

If you would like to remove the systemd units, do so with `machined remove`.
If for any reason machined fails, you can clean up the unit with `systemctl --user reset-failed machined.service`.
Then re-run the `machined remove` command to remove the units.


Note: on some systems, systemd-run --user prevents access to /dev/kvm via groups
The current workaround is to `sudo chmod 0666 /dev/kvm`

## Run machine client

```
./bin machine list
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


## Examples

See [doc/examples](doc/examples/) for other example VM definitions.
