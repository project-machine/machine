package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/apex/log"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var runCmd = cli.Command{
	Name: "run",
	Usage: "run a machine",
	Action: doRun,
}

func doRun(ctx *cli.Context) error {
	vmName := "default"
	if ctx.NArg() > 0 {
		vmName = ctx.Args()[0]
	}

	filename := filepath.Join(configDir, "machine", envDir, fmt.Sprintf("%s.yaml", vmName))
	contents, err := os.ReadFile(filename)
	if err != nil {
		return errors.Wrapf(err, "Error reading \"%s\"", filename)
	}

	var desc VMDef
	err = yaml.Unmarshal(contents, &desc)
	if err != nil {
		return errors.Wrapf(err, "Error parsing \"%s\"", filename)
	}

	// setup SIGINT handler to ensure deferred functions run on Control-C
	ch := make(chan os.Signal, 10)
	signal.Notify(ch, syscall.SIGINT)
	endCh := make(chan struct{}, 1)
	doneCh := make(chan struct{}, 1)

	// Once interrupt will exit the VMs, two will hard-exit possibly leaving
	// network bridge/interfaces around.
	go func() {
		<-ch
		fmt.Fprintf(os.Stderr, "^C pressed, pressing it again will kill the install ungracefully\n")
		<-ch

		endCh <- struct{}{}
		// trigger a panic to ensure deferred functions run before os.Exit
		panic("User interrupted machine-run")
	}()

	// where should we put sockfiles and tpm-dir?
	runDir := filepath.Join(dataDir, "machine", envDir, fmt.Sprintf("%s.rundir", vmName))
	vm, err := newVM(endCh, doneCh, desc, runDir)
	if err != nil {
		return errors.Wrapf(err, "Failed creating VM instance")
	}

	err = vm.Start()
	if err != nil {
		return errors.Wrapf(err, "Failed starting VM:%s", vm.Name)
	}

	log.Infof("Watching for VM:%s to exit", vm.Name)
	<-vm.doneCh
	log.Infof("VM:%s exited", vm.Name)

	log.Infof("Exiting")

	return finalErr
}

