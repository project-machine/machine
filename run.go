package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/apex/log"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var VMCounter sync.WaitGroup

var runCmd = cli.Command{
	Name:   "run",
	Usage:  "run a machine",
	Action: doRun,
}

var VMs = []*VM{}
var exitRequestCh chan(struct{})

func apiHandler(w http.ResponseWriter, req *http.Request) {
	fields := strings.Split(req.URL.Path[1:], "/")
	fmt.Printf("apiHandler: got %+v\n", req)
	n := len(fields)
	fmt.Printf("%d fields\n", n)
	defer req.Body.Close()

	if req.Method != "GET" {
		w.WriteHeader(404)
		return
	}
	switch fields[0] {
	case "exit":
		io.WriteString(w, "{status:\"Exiting\"}\n")
		os.Exit(1)
		exitRequestCh<-struct{}{}
		return
	case "status":
		io.WriteString(w, "{status:\"Running\"}\n")
		return
	case "machines":
		io.WriteString(w, "[")
		first := true
		for _, v := range VMs {
			if first {
				first = false
			} else {
				io.WriteString(w, ",")
			}
			s := fmt.Sprintf("{name:\"%s\"}", v.Name)
			io.WriteString(w, s)
		}
		io.WriteString(w, "\n")
		return
	default:
		w.WriteHeader(404)
		return
	}
}

func doRun(ctx *cli.Context) error {
	if ctx.NArg() == 0 {
		return errors.Errorf("VM name must be provided")
	}

	cluster := ctx.Args()[0]
	cPath := filepath.Join(configDir, "machine", cluster, "machine.yaml")
	contents, err := os.ReadFile(ConfPath(cluster))
	if err != nil {
		return errors.Wrapf(err, "Error reading \"%s\"", cPath)
	}

	var suite VMSuite
	err = yaml.Unmarshal(contents, &suite)
	if err != nil {
		return errors.Wrapf(err, "Error parsing \"%s\"", cPath)
	}

	// setup SIGINT handler to ensure deferred functions run on Control-C
	ch := make(chan os.Signal, 10)
	signal.Notify(ch, syscall.SIGINT)
	endCh := make(chan struct{}, 1)

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

	os.RemoveAll(DataDir(cluster))

	log.Infof("loading vms")
	for _, vm := range suite.Machines {
		// where should we put sockfiles and tpm-dir?
		vmName := vm.Name
		doneCh := make(chan struct{}, 1)
		VM, err := newVM(endCh, doneCh, vm, RunDir(cluster, vmName))
		if err != nil {
			return errors.Wrapf(err, "Failed creating VM instance")
		}

		VMs = append(VMs, &VM)
	}

	// Start a rest server
	listener, err := net.Listen("unix", DataDir(cluster) + "/api.sock")
	if err != nil {
		return err
	}
	defer listener.Close()
	exitRequestCh := make(chan struct{}, 1)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("v1/", apiHandler)
		if err = http.Serve(listener, mux); err != nil {
			log.Errorf("Error starting http server: %s", err)
			os.Exit(1)
		}
	}()

	log.Infof("starting vms")
	for _, vm := range VMs {
		err = vm.Start()
		if err != nil {
			return errors.Wrapf(err, "Failed starting VM:%s", vm.Name)
		}
		VMCounter.Add(1)

		go func(vm *VM) {
			log.Infof("Watching  for VM:%s to exit", vm.Name)
			<-vm.doneCh
			log.Infof("VM:%s exited", vm.Name)
			VMCounter.Done()
		}(vm)
	}

	go func() {
		VMCounter.Wait()
		log.Infof("All VMs done")
		endCh <- struct{}{}
	}()

	log.Infof("Waiting for an exit signal")
	select {
	case <-endCh:
		log.Infof("All VMs exited")
	case <-exitRequestCh:
		log.Infof("Exit requested by http API")
		endCh <- struct{}{}
	}

	log.Infof("%s exiting", cluster)
	return finalErr
}
