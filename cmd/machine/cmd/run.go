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
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:        "run <machine_name> <machine config>",
	Args:       cobra.MinimumNArgs(2),
	ArgAliases: []string{"machineName"},
	Short:      "create and start a new machine",
	Long:       `create a new machine from config and start the machine.`,
	Run:        doRun,
}

// Initialize a new machine from config file and then start it up
func doRun(cmd *cobra.Command, args []string) {
	machineName := args[0]
	machineConfig := args[1]
	editMachine := false

	// FIXME: handle mismatch between name in arg and value in config file
	if err := DoCreateMachine(machineName, defaultMachineType, machineConfig, editMachine); err != nil {
		panic(fmt.Sprintf("Failed to create machine '%s' from config '%s': %s", machineName, machineConfig, err))
	}

	if err := DoStartMachine(machineName); err != nil {
		panic(fmt.Sprintf("Failed to start machine '%s' from config '%s': %s", machineName, machineConfig, err))
	}
}

func init() {
	rootCmd.AddCommand(runCmd)
}
