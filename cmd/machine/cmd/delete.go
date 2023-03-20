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
	"machine/pkg/api"

	"github.com/spf13/cobra"
)

// deleteCmd represents the list command
var deleteCmd = &cobra.Command{
	Use:        "delete <machine_name>",
	Args:       cobra.MinimumNArgs(1),
	ArgAliases: []string{"machineName"},
	Short:      "delete the specified machine",
	Long:       `delete the specified machine if it exists`,
	Run:        doDelete,
}

func doDelete(cmd *cobra.Command, args []string) {
	machineName := args[0]
	endpoint := fmt.Sprintf("machines/%s", machineName)
	deleteURL := api.GetAPIURL(endpoint)
	if len(deleteURL) == 0 {
		panic("Failed to get DELETE API URL for 'machines' endpoint")
	}
	resp, err := rootclient.R().EnableTrace().Delete(deleteURL)
	if err != nil {
		fmt.Printf("Failed to delete machine '%s': %s\n", machineName, err)
		panic(err)
	}
	fmt.Println(resp.Status())
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}
