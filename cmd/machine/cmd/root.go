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
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"machine/pkg/api"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string
var rootclient *resty.Client

const (
	petNameWords = 2
	petNameSep   = "-"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "machine",
	Short: "The machine client is used to run and manage machine machines",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.client.yaml)")

	// configure the http client to point to the unix socket
	apiSocket := api.APISocketPath()
	if len(apiSocket) == 0 {
		panic("Failed to get API socket path")
	}

	unixDial := func(_ context.Context, network, addr string) (net.Conn, error) {
		raddr, err := net.ResolveUnixAddr("unix", apiSocket)
		if err != nil {
			return nil, err
		}

		return net.DialUnix("unix", nil, raddr)
	}

	transport := http.Transport{
		DialContext:           unixDial,
		DisableKeepAlives:     true,
		ExpectContinueTimeout: time.Second * 30,
		ResponseHeaderTimeout: time.Second * 3600,
		TLSHandshakeTimeout:   time.Second * 5,
	}

	rootclient = resty.New()
	rootclient.SetTransport(&transport).SetScheme("http").SetBaseURL(apiSocket)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".client" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".client")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

// common for all commands
func getMachines() ([]api.Machine, error) {
	machines := []api.Machine{}
	listURL := api.GetAPIURL("machines")
	if len(listURL) == 0 {
		return machines, fmt.Errorf("Failed to get API URL for 'machines' endpoint")
	}
	resp, _ := rootclient.R().EnableTrace().Get(listURL)
	err := json.Unmarshal(resp.Body(), &machines)
	if err != nil {
		return machines, fmt.Errorf("Failed to unmarshal GET on /machines")
	}
	return machines, nil
}

func getMachine(machineName string) (api.Machine, error) {
	machine := api.Machine{}
	getURL := api.GetAPIURL(filepath.Join("machines", machineName))
	if len(getURL) == 0 {
		return machine, fmt.Errorf("Failed to get API URL for 'machines/%s' endpoint", machineName)
	}
	resp, _ := rootclient.R().EnableTrace().Get(getURL)
	err := json.Unmarshal(resp.Body(), &machine)
	if err != nil {
		return machine, fmt.Errorf("Failed to unmarshal GET on /machines/%s", machineName)
	}
	return machine, nil
}

func postMachine(newMachine api.Machine) error {
	postURL := api.GetAPIURL("machines")
	if len(postURL) == 0 {
		return fmt.Errorf("Failed to get API URL for 'machines' endpoint")
	}
	resp, err := rootclient.R().EnableTrace().SetBody(newMachine).Post(postURL)
	if err != nil {
		return fmt.Errorf("Failed POST to 'machines' endpoint: %s", err)
	}
	fmt.Printf("%s %s\n", resp, resp.Status())
	return nil
}

func putMachine(newMachine api.Machine) error {
	endpoint := fmt.Sprintf("machines/%s", newMachine.Name)
	putURL := api.GetAPIURL(endpoint)
	if len(putURL) == 0 {
		return fmt.Errorf("Failed to get API PUT URL for 'machines' endpoint")
	}
	resp, err := rootclient.R().EnableTrace().SetBody(newMachine).Put(putURL)
	if err != nil {
		return fmt.Errorf("Failed PUT to machine '%s' endpoint: %s", newMachine.Name, err)
	}
	fmt.Printf("%s %s\n", resp, resp.Status())
	return nil
}
