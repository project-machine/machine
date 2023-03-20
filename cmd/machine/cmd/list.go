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
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list all of the defined machines",
	Long:  `list all of the defined machines`,
	Run:   doList,
}

func doList(cmd *cobra.Command, args []string) {
	machines, err := getMachines()
	if err != nil {
		panic(err)
	}
	tbl := table.New("Name", "Status", "Description")
	tbl.AddRow("----", "------", "-----------")
	for _, machine := range machines {
		tbl.AddRow(machine.Name, machine.Status, machine.Description)
	}
	tbl.Print()
}

func init() {
	rootCmd.AddCommand(listCmd)
	table.DefaultHeaderFormatter = func(format string, vals ...interface{}) string {
		return strings.ToUpper(fmt.Sprintf(format, vals...))
	}
}
