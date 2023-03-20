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
	"fmt"
	"machine/pkg/api"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "machined",
	Short: "A daemon to handle the lifecycle of machine machines",
	Long: `The daemon runs a RESTful service to interact with and manage
machine machines.`,
	Run: doServerRun,
}

func doServerRun(cmd *cobra.Command, args []string) {
	conf := api.DefaultMachineDaemonConfig()
	ctrl := api.NewController(conf)

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	log.Infof("machined starting up in %s", cwd)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := ctrl.Run(ctx); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()
	<-ctx.Done()
	log.Infof("machined shutting down gracefully, press Ctrl+C again to force")
	log.Infof("machined notifying all machines to shutdown... (FIXME)")
	log.Infof("machined waiting up to %s seconds\n", "30")
	if err := ctrl.MachineController.StopMachines(); err != nil {
		log.Errorf("Failure during machine shutdown: %s\n", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	ctrl.Shutdown(ctx)
	log.Infof("machined exiting")
}

// func doServerRun(cmd *cobra.Command, args []string) {
// 	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
// 	defer stop()
//
// 	unixSocket := api.APISocketPath()
// 	if len(unixSocket) == 0 {
// 		panic("Failed to get an API Socket path")
// 	}
// 	// mkdir -p on dirname(unixSocet)
// 	err := os.MkdirAll(filepath.Dir(unixSocket), 0755)
// 	if err != nil {
// 		panic(fmt.Sprintf("Failed to create directory path to: %s", unixSocket))
// 	}
// 	// FIXME to check if another machined is running/pidfile?, flock?
// 	if PathExists(unixSocket) {
// 		os.Remove(unixSocket)
// 	}
//
// 	fmt.Println("machined service running on: %s", unixSocket)
// 	router := gin.Default()
// 	router.GET("/machines", api.GetMachines)
// 	router.POST("/machines", api.PostMachines)
//
// 	// re-implement gin.Engine.RunUnix() so we can set the context ourselves
// 	listener, err := net.Listen("unix", unixSocket)
// 	if err != nil {
// 		panic("Failed to create a unix socket listener")
// 	}
// 	defer listener.Close()
// 	defer os.Remove(unixSocket)
//
// 	srv := &http.Server{Handler: router.Handler()}
// 	go func() {
// 		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
// 			panic(fmt.Sprintf("Failed to Serve: %s", err))
// 		}
// 	}()
//
// 	<-ctx.Done()
// 	// restore default behavior on the interrupt signal and nitify user of
// 	// shutdown
// 	fmt.Println("machined shutting down gracefully, press Ctrl+C again to force")
// 	fmt.Println("machined notifying all machines to shutdown... (FIXME)")
// 	fmt.Printf("machined waiting up to %s seconds\n", MachineShutdownTimeoutSeconds)
//
// 	ctx, cancel := context.WithTimeout(context.Background(), MachineShutdownTimeoutSeconds)
// 	defer cancel()
// 	if err := srv.Shutdown(ctx); err != nil {
// 		panic(fmt.Sprintf("machined forced to shutdown: %s", err))
// 	}
// 	fmt.Println("machined exiting")
// }

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// init our rng
	rand.Seed(time.Now().UTC().UnixNano())

	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.server.yaml)")
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

		// Search config in home directory with name ".server" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".server")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	// TODO:
	// parse the config
	// for each on-disk machine, read in the yaml and post the struct
}
