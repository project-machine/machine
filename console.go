package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli"
)

var consoleCmd = cli.Command{
	Name:   "console",
	Usage:  "open serial console to a VM",
	Action: doConsole,
}

func doConsole(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		return fmt.Errorf("cluster name is required argument")
	}
	ret := strings.SplitN(ctx.Args()[0], ":", 2)
	cluster := ret[0]
	vmName := ret[1]
	sockPath := SockPath(cluster, vmName, "console.socket")
	cmd := exec.Command("nc", "-U", sockPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var guiCmd = cli.Command{
	Name:   "gui",
	Usage:  "open graphical connection to a VM",
	Action: doGui,
}

func doGui(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		return fmt.Errorf("vm name is required argument")
	}
	arg0 := ctx.Args()[0]
	ret := strings.SplitN(arg0, ":", 2)
	cluster := "default"
	vmName := arg0
	if len(ret) == 2 {
		cluster = ret[0]
		vmName = ret[1]
	}
	portPath := filepath.Join(RunDir(cluster, vmName), "gui.port")
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
