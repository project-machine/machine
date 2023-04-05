package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"machine/pkg/api"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/template"
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

// common bits for install/remove commands
const (
	HostSystemdUnitPath = "/etc/systemd/system"
	MachinedServiceUnit = "machined.service"
	MachinedSocketUnit  = "machined.socket"
)

const MachinedServiceTemplate = `[Unit]
Description=Machined Service
Requires=machined.socket
After=machined.socket
StartLimitIntervalSec=0

[Service]
Delegate=true
Type=exec
KillMode=process
ExecStart={{.MachinedBinaryPath}}

[Install]
WantedBy=default.target
`

const MachinedSocketTemplate = `
[Unit]
Description=Machined Socket

[Socket]
ListenStream=%t/machined/machined.socket
SocketMode=0660

[Install]
WantedBy=sockets.target
`

func getSystemdUnitPath(hostMode bool) (string, error) {
	if hostMode {
		return HostSystemdUnitPath, nil
	}
	ucd, err := api.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("Failed to get UserConfigDir via API: %s", err)
	}
	return filepath.Join(ucd, "systemd/user"), nil
}

// GetTemplate returns a template string, either from a specified file or default template
func getTemplate(cliTemplateFile, defaultTemplate string) string {
	if cliTemplateFile != "" {
		content, err := ioutil.ReadFile(cliTemplateFile)
		if err == nil {
			return string(content)
		}
	}
	return defaultTemplate
}

func getMachinedBinaryPath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("Failed to determine machined full path: %s", err)
	}
	return path, nil
}

func installTemplate(templateSource, target string) error {
	machinedPath, err := getMachinedBinaryPath()
	if err != nil {
		return fmt.Errorf("Failed to get path to machined binary: %s", err)
	}

	binpath := struct {
		MachinedBinaryPath string
	}{
		MachinedBinaryPath: machinedPath,
	}

	tpl := template.New("systemd-unit")
	tpl, err = tpl.Parse(templateSource)
	if err != nil {
		return fmt.Errorf("Failed to parse provided template for target %q", target)
	}

	fh, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("Failed to create target file %q: %s", target, err)
	}
	log.Infof("Installing %s", target)
	return tpl.Execute(fh, binpath)
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
