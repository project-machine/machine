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
package main

import (
	"fmt"
	"os"

	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/termios"
	"github.com/project-machine/machine/pkg/api"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v2"
)

// editCmd represents the edit command
var editCmd = &cobra.Command{
	Use:        "edit <machine name>",
	Args:       cobra.MinimumNArgs(1),
	ArgAliases: []string{"machineName"},
	Short:      "edit a machine's configuration file",
	Long:       `Read the machine configuration into an editor for modification`,
	Run:        doEdit,
}

// edit requires one to:
// - GET the machine configuration from REST API
// - render this to a temp file
// - invoke $EDITOR to allow user to make changes
//
// Option 1:
// - (optionally) before posting, run JSON validator on the new file?
// - PATCH/UPDATE the machine configuration back to API
//   (and symantically what does that mean if the instance is running)
//
// Option 2:
// - write out changes to config file on disk and not modifying in-memory state
//   via PATCH/UPDATE operations.
//
func doEdit(cmd *cobra.Command, args []string) {
	machineName := args[0]
	machines, err := getMachines()
	if err != nil {
		panic(err)
	}

	var machineBytes []byte
	onTerm := termios.IsTerminal(unix.Stdin)
	editMachine := &api.Machine{}

	for _, machine := range machines {
		if machine.Name == machineName {
			editMachine = &machine
			break
		}
	}
	if editMachine.Name == "" {
		panic(fmt.Sprintf("Failed to find machine '%s'", machineName))
	}

	machineBytes, err = yaml.Marshal(editMachine)
	if err != nil {
		panic(fmt.Sprintf("Error marshalling machine '%s'", machineName))
	}

	machineBytes, err = shared.TextEditor("", machineBytes)
	if err != nil {
		panic("Error calling editor")
	}

	newMachine := api.Machine{Name: machineName}
	for {
		err = yaml.Unmarshal(machineBytes, &newMachine)
		if err == nil {
			pErr := checkMachineFilePaths(&newMachine)
			if pErr == nil {
				break
			}
			fmt.Printf("Error checking paths in config: %s\n", pErr)
		}
		if !onTerm {
			panic(fmt.Sprintf("Error parsing configuration: %s", err))
		}
		fmt.Printf("Error parsing yaml: %v\n", err)
		fmt.Println("Press enter to re-open editor, or ctrl-c to abort")
		_, err := os.Stdin.Read(make([]byte, 1))
		if err != nil {
			panic(fmt.Sprintf("Error reading reply: %s", err))
		}
		machineBytes, err = shared.TextEditor("", machineBytes)
		if err != nil {
			panic(fmt.Sprintf("Error calling editor: %s", err))
		}

	}
	// persist config if not ephemeral

	err = putMachine(newMachine)
	if err != nil {
		panic(err.Error())
	}
}

func init() {
	rootCmd.AddCommand(editCmd)
}
