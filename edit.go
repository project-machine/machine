package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/termios"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v2"
)

var editCmd = cli.Command{
	Name:   "edit",
	Usage:  "edit a machine definition",
	Action: doEdit,
}

func doEdit(ctx *cli.Context) error {
	if ctx.NArg() == 0 {
		return errors.Errorf("VM name must be provided")
	}

	cluster := ctx.Args()[0]
	cPath := ConfPath(cluster)

	var vmbytes []byte
	var err error
	if !termios.IsTerminal(unix.Stdin) {
		return errors.Errorf("Not on a terminal")
	}

	vmbytes, err = os.ReadFile(cPath)
	if err != nil {
		return errors.Wrapf(err, "Error reading definition from %s", cPath)
	}

	changed := false
	for {
		n, err := shared.TextEditor("", vmbytes)
		if err != nil {
			return errors.Wrapf(err, "Error calling editor")
		}
		if bytes.Compare(vmbytes, n) != 0 {
			changed = true
			vmbytes = n
		}

		if !changed {
			break
		}

		var vm VMDef
		if err = yaml.Unmarshal(vmbytes, &vm); err == nil {
			break
		}

		fmt.Printf("Error parsing yaml: %v\n", err)
		fmt.Println("Press enter to re-open editor, or ctrl-c to abort")
		if _, err = os.Stdin.Read(make([]byte, 1)); err != nil {
			return errors.Wrapf(err, "Error reading reply")
		}
	}

	if !changed {
		fmt.Println("No changes made")
		return nil
	}

	if err = os.WriteFile(cPath, vmbytes, 0600); err != nil {
		return errors.Wrapf(err, "Error saving configuration")
	}

	return nil
}
