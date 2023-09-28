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

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:        "start <machine_name>",
	Args:       cobra.MinimumNArgs(1),
	ArgAliases: []string{"machineName"},
	Short:      "start the specified machine",
	Long:       `start the specified machine if it exists`,
	Run:        doStart,
}

// Ideally:
// 	starting a machine requires POST'ing an update to the machine state
// 	which toggles from the 'stopped' state to the 'running' state.
// 	Asynchronously the machine will start, in a separate goroutine spawned by
// 	machined, and depending on client flags (blocking/non-blocking) the server
// 	will return back an new URL for status on the machine instance
//
// TBD, the affecting the machines in each machine
//
// Currently we now post a request with {'status': 'running'} to start a machine

func doStart(cmd *cobra.Command, args []string) {
	machineName := args[0]
	if err := DoStartMachine(machineName); err != nil {
		panic(fmt.Sprintf("Failed to start machines '%s': %s", machineName))
	}
}

func DoStartMachine(machineName string) error {
	fmt.Printf("Starting machine %s\n", machineName)
	var request struct {
		Status string `json:"status"`
	}
	request.Status = "running"
	endpoint := fmt.Sprintf("machines/%s/start", machineName)
	startURL := api.GetAPIURL(endpoint)
	if len(startURL) == 0 {
		return fmt.Errorf("Failed to get API URL for 'machines/%s/start' endpoint", machineName)
	}
	resp, err := rootclient.R().EnableTrace().SetBody(request).Post(startURL)
	if err != nil {
		return fmt.Errorf("Failed POST to 'machines/%s/start' endpoint: %s", machineName, err)
	}
	fmt.Printf("%s %s\n", resp, resp.Status())
	return nil
}

func init() {
	rootCmd.AddCommand(startCmd)
}
