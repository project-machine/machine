package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/urfave/cli"
)

var consoleCmd = cli.Command{
	Name: "console",
	Usage: "open serial console to a VM",
	Action: doConsole,
}

func doConsole(ctx *cli.Context) error {
	// TODO - get this path from the daemon over REST api
	if ctx.NArg() < 1 {
		return fmt.Errorf("VM name is required argument")
	}
	vmName := ctx.Args()[0]
	sockPath := filepath.Join(dataDir,
		 "machine",
		 envDir,
		 fmt.Sprintf("%s.rundir", vmName),
		 "sockets",
		 "console.socket")
	cmd := exec.Command("nc", "-U", sockPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var guiCmd = cli.Command{
	Name: "gui",
	Usage: "open graphical connection to a VM",
	Action: doGui,
}

func doGui(ctx *cli.Context) error {
	// TODO - get this path from the daemon over REST api
	if ctx.NArg() < 1 {
		return fmt.Errorf("VM name is required argument")
	}
	vmName := ctx.Args()[0]
	portPath := filepath.Join(dataDir,
		 "machine",
		 envDir,
		 fmt.Sprintf("%s.rundir", vmName),
		 "gui.port")
	c, err := os.ReadFile(portPath)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(string(c)) // check that it's valid
	if err != nil {
		return err
	}
	if port < 5900 || port > 6000 {
		return fmt.Errorf("invalid port: %s", string(c))
	}
	cmd := exec.Command("spicy", "-h", "localhost", "-p", string(c))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
