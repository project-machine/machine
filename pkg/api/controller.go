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
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/coreos/go-systemd/activation"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type Controller struct {
	Config            *MachineDaemonConfig
	Router            *gin.Engine
	MachineController MachineController
	Server            *http.Server
	wgShutDown        *sync.WaitGroup
	portNumber        int
}

func NewController(config *MachineDaemonConfig) *Controller {
	var controller Controller

	controller.Config = config
	controller.wgShutDown = new(sync.WaitGroup)

	return &controller
}

func (c *Controller) Run(ctx context.Context) error {
	// load existing machines
	machineDir := filepath.Join(c.Config.ConfigDirectory, "machines")
	if PathExists(machineDir) {
		log.Infof("Loading saved machine configs...")
		err := filepath.Walk(machineDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				machineConf := filepath.Join(path, "machine.yaml")
				if PathExists(machineConf) {
					newMachine, err := LoadConfig(machineConf)
					if err != nil {
						return err
					}
					newMachine.ctx = c.Config.GetConfigContext()
					log.Infof("  loaded machine %s", newMachine.Name)
					c.MachineController.Machines = append(c.MachineController.Machines, newMachine)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	unixSocket := APISocketPath()
	if len(unixSocket) == 0 {
		panic("Failed to get an API Socket path")
	}
	log.Infof("Using machined API socket: %s", unixSocket)

	// mkdir -p on dirname(unixSocet)
	err := os.MkdirAll(filepath.Dir(unixSocket), 0755)
	if err != nil {
		panic(fmt.Sprintf("Failed to create directory path to: %s", unixSocket))
	}

	// handle systemd socket activation
	listeners, err := activation.Listeners()
	if err != nil {
		panic(err)
	}

	// configure engine, router, and server
	engine := gin.Default()
	c.Router = engine
	_ = NewRouteHandler(c)
	c.Server = &http.Server{Handler: c.Router.Handler()}

	// either systemd socket unit isn't started or we're not using systemd
	if len(listeners) > 0 {
		for _, listener := range listeners {
			if listener != nil {
				log.Infof("machined service starting service via socket activation")
				return c.Server.Serve(listeners[0])
			}
		}
	}
	log.Infof("No systemd socket activation, falling back on direct listen")

	// FIXME to check if another machined is running/pidfile?, flock?
	if PathExists(unixSocket) {
		os.Remove(unixSocket)
	}
	defer os.Remove(unixSocket)

	// re-implement gin.Engine.RunUnix() so we can set the context ourselves
	listener, err := net.Listen("unix", unixSocket)
	if err != nil {
		panic("Failed to create a unix socket listener")
	}
	defer listener.Close()

	return c.Server.Serve(listener)
}

func (c *Controller) InitMachineController(ctx context.Context) error {
	c.MachineController = MachineController{}

	// TODO
	// look for serialized Machine configuration files in data dir
	// for each one, read them in and add to the Controller
	return nil
}

func (c *Controller) Shutdown(ctx context.Context) error {
	c.wgShutDown.Wait()
	if err := c.Server.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
