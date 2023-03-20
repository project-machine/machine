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
	Run:   doInfo,
}

func doInfo(cmd *cobra.Command, args []string) {
	machineName := args[0]
	machine, err := getMachine(machineName)
	if err != nil {
		panic(err)
	}

	machineBytes, err := yaml.Marshal(machine)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s", machineBytes)
}

func init() {
	rootCmd.AddCommand(infoCmd)
	table.DefaultHeaderFormatter = func(format string, vals ...interface{}) string {
		return strings.ToUpper(fmt.Sprintf(format, vals...))
	}
}
