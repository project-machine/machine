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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
	"github.com/project-machine/machine/pkg/api"
	"github.com/spf13/cobra"
)

// consoleCmd represents the console command
var consoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Connect to machine console (serial or graphical)",
	Long:  `Connect to machine text console (serial) or graphical`,
	Run:   doConsole,
}

func init() {
	rootCmd.AddCommand(consoleCmd)
	consoleCmd.PersistentFlags().StringP("console-type", "t", "", "console or vga")
}

// POST /machines/:machine/console '{"ConsoleType": "console|vga"}'
// RESP
// {
//  "Type": "console",
//  "Path": "$HOME/.../:machine/serial.sock"
// }
// {
//  "Type": "vga",
//  "Addr": "127.0.0.1",
//  "Port": "5901",
//  "Secure": false,
// }
func doConsole(cmd *cobra.Command, args []string) {
	consoleType := cmd.Flag("console-type").Value.String()
	if consoleType == "" {
		consoleType = api.SerialConsole
	}
	if consoleType != api.SerialConsole && consoleType != api.VGAConsole {
		panic(fmt.Sprintf("Invalid console type '%s'", consoleType))
	}
	if len(args) < 1 {
		panic("Missing required machine name")
	}
	machineName := args[0]

	consoleInfo, err := GetMachineConsoleInfo(machineName, consoleType)
	if err != nil {
		panic(err)
	}

	err = DoConsoleAttach(machineName, consoleInfo)
	if err != nil {
		panic(err)
	}
}

func GetMachineConsoleInfo(machineName, consoleType string) (api.ConsoleInfo, error) {
	consoleInfo := api.ConsoleInfo{}

	request := api.MachineConsoleRequest{ConsoleType: consoleType}
	endpoint := fmt.Sprintf("machines/%s/console", machineName)
	consoleURL := api.GetAPIURL(endpoint)
	if len(consoleURL) == 0 {
		return consoleInfo, fmt.Errorf("Failed to get API URL for 'machines/%s/console'", machineName)
	}

	resp, err := rootclient.R().EnableTrace().SetBody(request).Post(consoleURL)
	if err != nil {
		return consoleInfo, fmt.Errorf("Failed POST to %s: %s", endpoint, err)
	}
	fmt.Printf("%s %s\n", resp, resp.Status())

	err = json.Unmarshal(resp.Body(), &consoleInfo)
	if err != nil {
		return consoleInfo, fmt.Errorf("Failed to unmarshal response from %s: %s", endpoint, err)
	}

	return consoleInfo, nil
}

func doConsoleAttach(machineName string, consoleInfo api.ConsoleInfo) error {
	if consoleInfo.Path == "" {
		return fmt.Errorf("Invalid ConsoleInfo, Path is empty")
	}

	// 0x1d => ]
	args := []string{"stdin,echo=0,raw,escape=0x1d", fmt.Sprintf("unix-connect:%s", consoleInfo.Path)}
	cmd := exec.Command("socat", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Infof("Running command: %s", cmd.Args)
	fmt.Printf("Attaching to %s serial console, use 'Control-]' to detatch from console\n", machineName)
	return cmd.Run()
}

func doVGAAttach(machineName string, consoleInfo api.ConsoleInfo) error {

	args := []string{fmt.Sprintf("--host=%s", consoleInfo.Addr)}
	if consoleInfo.Secure {
		args = append(args, fmt.Sprintf("--secure-port=%s", consoleInfo.Port))
	} else {
		args = append(args, fmt.Sprintf("--port=%s", consoleInfo.Port))
	}
	args = append(args, fmt.Sprintf("--title='machine %s'", machineName))

	cmd := exec.Command("spicy", args...)
	fmt.Printf("Attaching to %s vga console\n", machineName)
	return cmd.Run()
}

func DoConsoleAttach(machineName string, consoleInfo api.ConsoleInfo) error {
	switch consoleInfo.Type {
	case api.SerialConsole:
		return doConsoleAttach(machineName, consoleInfo)
	case api.VGAConsole:
		return doVGAAttach(machineName, consoleInfo)
	default:
		return fmt.Errorf("Cannot attach to unknown console type '%s'", consoleInfo.Type)
	}

	return nil
}
