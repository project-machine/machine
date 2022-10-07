# Machine

## Introduction

Machine is a tool(set) to create, install, and run your container
images in a secure manner.

## Status

Currently all machine does is run kvm vms.  However, it does so
easily, driven by yaml specs, using secureboot and a UEFI db
configured by yourself.

### Example 1
cat > vm1.yaml << EOF
name: vm1
type: kvm
cdrom: jammy-desktop-amd64.iso
boot: cdrom
secure-boot: true
gui: true
tpm: true
tpm-version: 2
disks:
  - file: /home/serge/vm1-disk.qcow2
    type: "ssd"
EOF

machine init vm1 < vm1.yaml
machine run vm1
machine gui vm1

### Example 2
cat > vm2.yaml << EOF
name: vm2
type: kvm
cdrom: /home/serge/heimdall-0.0.10-snakeoil.iso
boot: cdrom
secure-boot: true
uefi-vars: /home/serge/ovmf_vars.fd
tpm: true
tpm-version: 2 # this is the default
disks:
  - file: /home/serge/vm2-disk.qcow2
    type: "ssd"
EOF

machine init vm2 < vm2.yaml
machine run vm2
machine console vm2  # wait for results
machine edit vm2 --cdrom=puzzleos-installer.iso
machine run vm2
machine console vm2  # wait for results
machine edit vm2 --boot=disk
machine run vm2
machine console vm2  # use the vm

### Example 3

cat > machine.yaml << EOF
type: kvm

networks:
  - name: net1
    type: user

connections:
  vm1:
    nic1: net1
  vm2:
    nic1: net1

machines:
  - name: vm1
    cdrom: /home/serge/jammy-desktop-amd64.iso
    boot: cdrom # default is disk
    secure-boot: true
    tpm: true
    tpm-version: 2 # this is the default
    disks:
      - file: /home/serge/vm1-disk.qcow2
        type: "ssd"
    nics:
      - id: nic1

  - name: vm1
    cdrom: /home/serge/heimdall-0.0.10-snakeoil.iso
    #cdrom: /home/serge/barehost-atomfs.iso
    boot: cdrom # default is disk
    secure-boot: true
    uefi-vars: /home/serge/ovmf_vars.fd
    tpm: true
    tpm-version: 2 # this is the default
    disks:
      - file: /home/serge/vm2-disk.qcow2
        type: "ssd"
    nics:
      - id: nic1
EOF
machine init --env cluster1 --import machine.yaml

# Goal

The above examples show how the current minimal machine can be used.
However, the machine project also includes a kernel, RFS, and image
builder which will provide secureboot-protected, remote-attestation
capable running of your VM images.  Using an image from zothub.io,
local oci, or elsewhere, simply

machine init --env cluster1 pxehost.yaml
machine attest cluster1
