# Example machine cli commands


## launch (init + run)
```
machine launch images:ubuntu/22.04 vm1
```

Expanded

machine init \
    --name=vm1 \
    --root-disk \  # source:dest:size:driver:block_size:devopts
        http://c.i.u/ubuntu-server/daily/current/22.04:\
        ~/.local/share/machine/vm1/disks/root-disk.img:\
        100G:\
        virtio-blk-pci:\
        512:\
        serial=srcfile-size-format,bootindex=0 \
    --extra-disk \ # <disk format>
    --memory 1024M \
    --smp 2 \
    --uefi \
    --enable-tmp \
    --tpm-version 2.0
    --network \  # alias | name:type:id(empty autoallocate),netopts
         default:user:net0:hostfwd=tcp::<hostport>-:22 \
    --nic \  # driver:network:devopts
        virtio-net-pci::bootindex \
    --save-config ~/.config/machine/vm1/machine.yaml
    --run


Create a default machine:

- smp 2
- mem 1024
- serial-console on pty, machine console vm1
- virtio-net nic on -net user
- acquire root-disk, and boot from it
- boot via uefi with tmp 2.0 and secure-boot
- write config to CONFIG path for the machine
- headless by default (enable vnc/spice)
- enable ssh forwarding, machine ssh vm1


## machine networking

`network --type user citra`

    default, guest to host, no-guest-to-gest

`network --type host-bridge --interface br1 cascade`

   Attach nics using this network to an existing host bridge, this requires
   sudo privs when launching.

`network --type user-bridge fuggle`

  create a network-namespace called XX, in which a bridge is created,
  spawn pasta on host connecting to this namespace to allow network traffic
  to flow to the host network (and off box)

  if no VMs in bridge XX are running, then pasta is stopped and NS is removed.
  when any VM in bridge XX then a new NS is created and pasta launched.


# PXE/ZOT Client scenario

```
machine create network --type user-bridge fuggle
machine launch images:pxeserver:v1.2 pxe-server --network fuggle --cloud-cfg pxe.cfg
machine launch images:zot:v1.0 z1 --extra-disk 500G --network fuggle --cloud-cfg zot.cfg
machine launch --empty-disk 100G --network-fuggle
```

```
machine launch img foobar --nic device=virtio-net network=foo --nic device=e1000 network=bar
```


```
$ cat .config/machine/pxe-server/machine.yaml
type: kvm
ephemeral: false
description: pxe-server for booting other machiens
name: pxe-server
config:
    cpus: 2
    memory: 2048M
    uefi: true
    secureboot: true
    tpm: true
    tpm-version: 2.0
    truststore: $XDG_DATA_DIR/machine/trust/project1
    disks:
        - file: $XDG_DATA_DIR/machine/pxe-server/root-disk.qcow2
        type: ssd
        attach: virtio
        bootindex: 0
    nics:
        - device: virtio-net
        network: fuggle
        mac: "random"
        id: nic0
        bootindex: 1
    config: |
        #cloud-config
        ...

$ cat .config/machine/zot/machine.yaml
type: kvm
ephemeral: false
description: zot oci service image
name: zot
config
    cpus: 2
    memory: 2048M
    uefi: true
    secureboot: true
    tpm: true
    tpm-version: 2.0
    truststore: $XDG_DATA_DIR/machine/trust/project1
    disks:
        - file: $XDG_DATA_DIR/machine/zot/01-root-disk.qcow2
        type: ssd
        attach: virtio
        bootindex: 0
        - file: $XDG_DATA_DIR/machine/zot/02-extra-disk.qcow2
        type: ssd
        attach: virtio
        bootindex: 1
        size: 500G
        cache: none
    nics:
        - device: virtio-net
        mac: "random"
        id: nic0
        bootindex: 2
        network: fuggle
    config: |
        #cloud-config
        ...

```
