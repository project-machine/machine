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
	"net/http"
	"strings"

	table "github.com/rodaine/table"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// infoCmd represents the info command
var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "info about the specified machine",
	Long:  `info about the specified machine`,
	RunE:  doInfo,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmd.SilenceUsage = true
	},
}

func doInfo(cmd *cobra.Command, args []string) error {
	machineName := args[0]
	machine, status, err := getMachine(machineName)
	if err != nil {
		return fmt.Errorf("Error getting machine '%s': %s", machineName, err)
	}
	if status != http.StatusOK {
		if status == http.StatusNotFound {
			// fmt.Printf("No such machine '%s'\n", machineName)
			return fmt.Errorf("No such machine '%s'", machineName)
		}
		return fmt.Errorf("Error: %d %v\n", status, err)
	} else {
		machineBytes, err := yaml.Marshal(machine)
		if err != nil {
			return fmt.Errorf("Failed to marshal response: %v", err)
		}
		fmt.Printf("%s", machineBytes)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(infoCmd)
	table.DefaultHeaderFormatter = func(format string, vals ...interface{}) string {
		return strings.ToUpper(fmt.Sprintf(format, vals...))
	}
}
