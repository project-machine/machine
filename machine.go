package main

import (
	"fmt"
	"os"

	"github.com/apex/log"
	"github.com/urfave/cli"
)

var Version = "0.0.1"
var configDir = ""
var dataDir = ""

func main() {
	app := cli.NewApp()
	app.Name = "machine"
	app.Usage = "runs a machine"
	app.Version = Version
	app.Commands = []cli.Command{
		consoleCmd,
		editCmd,
		guiCmd,
		initCmd,
		listCmd,
		runCmd,
	}

	var err error
	configDir, err = os.UserConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed getting user config dir: %v", err)
		os.Exit(1)
	}

	dataDir, err = UserDataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed getting user data dir: %v", err)
		os.Exit(1)
	}

	debug := false
	app.Before = func(c *cli.Context) error {
		if c.Bool("debug") {
			debug = true
			log.SetLevel(log.DebugLevel)
		}
		return nil
	}

	if err := app.Run(os.Args); err != nil {
		format := "error: %v\n"
		if debug {
			format = "error: %+v\n"
		}

		fmt.Fprintf(os.Stderr, format, err)
		os.Exit(1)
	}
}

