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
	"github.com/spf13/cobra"

	"github.com/project-machine/machine/pkg/api"
)

// guiCmd represents the gui command
var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "launch a gui client attaching to a specified machine",
	Run:   doGui,
}

func doGui(cmd *cobra.Command, args []string) {
	if len(args) < 1 {
		panic("Missing required machine name")
	}
	machineName := args[0]

	consoleInfo, err := GetMachineConsoleInfo(machineName, api.VGAConsole)
	if err != nil {
		panic(err)
	}

	err = DoConsoleAttach(machineName, consoleInfo)
	if err != nil {
		panic(err)
	}
}

func init() {
	rootCmd.AddCommand(guiCmd)
}
