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

	"github.com/project-machine/machine/pkg/api"
	"github.com/spf13/cobra"
)

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:        "stop <machine_name>",
	Args:       cobra.MinimumNArgs(1),
	ArgAliases: []string{"machineName"},
	Short:      "stop the specified machine",
	Long:       `stop the specified machine if it exists`,
	Run:        doStop,
}

// need to see about stopping single machine under machine and whole machine
func doStop(cmd *cobra.Command, args []string) {
	machineName := args[0]
	// Hi cobra, this is awkward...  why isn't there .Value.Bool()?
	forceStop, _ := cmd.Flags().GetBool("force")
	var request struct {
		Status string `json:"status"`
		Force  bool   `json:"force"`
	}
	request.Status = "stopped"
	request.Force = forceStop

	endpoint := fmt.Sprintf("machines/%s/stop", machineName)
	stopURL := api.GetAPIURL(endpoint)
	if len(stopURL) == 0 {
		panic(fmt.Sprintf("Failed to get API URL for 'machines/%s/stop' endpoint", machineName))
	}
	resp, err := rootclient.R().EnableTrace().SetBody(request).Post(stopURL)
	if err != nil {
		panic(fmt.Sprintf("Failed POST to 'machines/%s/stop' endpoint: %s", machineName, err))
	}
	fmt.Printf("%s %s\n", resp, resp.Status())
}

func init() {
	rootCmd.AddCommand(stopCmd)
	stopCmd.PersistentFlags().BoolP("force", "f", false, "shutdown the machine forcefully")
}
