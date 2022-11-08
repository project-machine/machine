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
	"time"

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
var exitRequestCh chan (struct{})
var exitHttpCh chan (struct{})

func apiHandler(w http.ResponseWriter, req *http.Request) {
	terminate := false
	fields := strings.Split(req.URL.Path[1:], "/")
	fmt.Printf("apiHandler: got %+v\n", req)
	n := len(fields)
	fmt.Printf("%d fields: %v, field[0]: %s\n", n, fields, fields[0])

	defer func() {
		fmt.Println("deferred req.Body.Close()\n")
		req.Body.Close()
	}()

	if req.Method != "GET" {
		w.WriteHeader(404)
		return
	}
	switch fields[0] {
	case "exit":
		fmt.Printf("handling exit\n")
		n, err := io.WriteString(w, "{\"status\":\"Exiting\"}\n")
		if err != nil || n < 21 {
			fmt.Printf("Failed writing string to http.Response: err:%w n=%d\n", err, n)
			return
		}
		fmt.Printf("Sending exit to exitRequestCh\n")
		exitRequestCh <- struct{}{}
	case "status":
		io.WriteString(w, "{\"status\":\"Running\"}\n")
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
			s := fmt.Sprintf("{\"name\":\"%s\"}", v.Name)
			io.WriteString(w, s)
		}
		io.WriteString(w, "\n")
		return
	default:
		w.WriteHeader(404)
		return
	}
	if terminate {
		fmt.Printf("terminating apiHandler\n")
		time.Sleep(time.Second * 3)
		os.Exit(0)
		//runtime.Goexit()
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
	endCh := make(chan struct{}, 2)

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

	log.Infof("loading vms")
	for _, vm := range suite.Machines {

		if vmConns, ok := suite.Connections[vm.Name]; ok {
			for nicID, networkName := range vmConns {
				foundNetwork := false
				for netidx := range suite.Networks {
					network := suite.Networks[netidx]
					if network.Name == networkName {
						foundNetwork = true
						break
					}
				}
				if !foundNetwork {
					return fmt.Errorf("Connection nicID:%s specified unknown network: %s", nicID, networkName)
				}
				foundNic := false
				for nidx := range vm.Nics {
					vmNic := vm.Nics[nidx]
					if nicID == vmNic.ID {
						foundNic = true
						vmNic.Network = networkName
						log.Debugf("Connecting %s.%s -> Network=%s", vm.Name, nicID, networkName)
						vm.Nics[nidx] = vmNic
						break
					}
				}
				if !foundNic {
					return fmt.Errorf("A connection for VM %s references undefined NIC %s", vm.Name, nicID)
				}
			}
		}

		// where should we put sockfiles and tpm-dir?
		vmName := vm.Name
		doneCh := make(chan struct{}, 1)
		VM, err := newVM(endCh, doneCh, vm, RunDir(cluster, vmName))
		if err != nil {
			return errors.Wrapf(err, "Failed creating VM instance")
		}

		log.Debugf("Adding nics...")
		for nidx := range vm.Nics {
			vmNic := vm.Nics[nidx]
			log.Debugf("Adding nic %s network %s", vmNic.ID, vmNic.Network)
			var nicNetwork NetworkDef
			for netidx := range suite.Networks {
				network := suite.Networks[netidx]
				if network.Name == vmNic.Network {
					nicNetwork = network
					break
				}
			}
			if len(nicNetwork.Name) == 0 {
				return fmt.Errorf("Unable to find the requested network %s for nic %s", vmNic.Network, vmNic.ID)
			}
			if err := VM.AddNic(&vmNic, &nicNetwork); err != nil {
				return errors.Wrapf(err, "Failed to add nic %s to network %s for vm %s", vmNic.ID, vmNic.Network, vm.Name)
			}
		}

		VMs = append(VMs, &VM)
	}

	// Start a rest server
	apiSock := ApiSockPath(cluster)
	if PathExists(apiSock) {
		fmt.Printf("Removing stale API socket: %s\n", apiSock)
		os.Remove(apiSock)
	}
	listener, err := net.Listen("unix", apiSock)
	if err != nil {
		return err
	}
	defer listener.Close()
	exitRequestCh = make(chan struct{}, 1)
	exitHttpCh = make(chan struct{}, 1)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("v1/", apiHandler)
		if err = http.Serve(listener, mux); err != nil {
			log.Errorf("Error starting http server: %s", err)
			os.Exit(1)
		}
		for {
			log.Infof("http API Server waiting on Exit Request")
			select {
			case <-exitHttpCh:
				log.Infof("apiHandler: Exit requested by http API, closing server")
				os.Exit(1)
			}
		}
	}()

	log.Infof("starting vms")
	defer StopVMs(VMs)

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
		log.Infof("after notifying endCh, os.exiting..")
		if finalErr != nil {
			log.Errorf("runcluster failed: %s", finalErr)
			os.Exit(1)
		}
		os.Exit(0)
	}()

	log.Infof("Waiting for an exit signal")
	terminate := false
	for {
		if terminate {
			log.Infof("final run select loop terminating...")
			break
		}
		log.Infof("runloop: waiting on channel (exit, end)..")
		select {
		case <-exitRequestCh:
			log.Infof("Exit requested by http API")
			log.Infof("Telling http API to stop...")
			exitHttpCh <- struct{}{}
			log.Infof("Stopping cluster...")
			StopVMs(VMs)
		case <-endCh:
			log.Infof("All VMs exited")
			terminate = true
			break
		}
		log.Info("runloop: after select")
	}

	log.Infof("%s exiting", cluster)
	return finalErr
}
