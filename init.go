package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/apex/log"
	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/termios"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v2"
)

var initCmd = cli.Command{
	Name: "init",
	Usage: "initialize a new machine from yaml",
	Action: doInit,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name: "file",
			Usage: "yaml file to import.  If unspecified, use stdin",
		},
		cli.BoolFlag{
			Name: "edit",
			Usage: "edit the yaml file inline",
		},
	},
}

func doInit(ctx *cli.Context) error {
	if ctx.NArg() == 0 {
		return errors.Errorf("Cluster name must be provided")
	}

	cluster := ctx.Args()[0]
	cPath := ConfPath(cluster)
	if PathExists(cPath) {
		return errors.Errorf("%s already defined", cluster)
	}

	var vmbytes []byte
	var err error
	onTerm := termios.IsTerminal(unix.Stdin)
	edit := ctx.Bool("edit")

	if edit && !onTerm {
		log.Infof("Aborting edit since stdin is not a terminal")
		edit = false
	}

	defpath := ctx.String("file")
	if defpath == "" {
		vmbytes, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			return errors.Wrapf(err, "Error reading definition from stdin")
		}
	} else {
		vmbytes, err = os.ReadFile(defpath)
		if err != nil {
			return errors.Wrapf(err, "Error reading definition from %s", defpath)
		}
	}

	if edit {
		vmbytes, err = shared.TextEditor("", vmbytes)
		if err != nil {
			return errors.Wrapf(err, "Error calling editor")
		}
	}

	for {
		var vm VMDef
		if err = yaml.Unmarshal(vmbytes, &vm); err == nil {
			break
		}
		if !onTerm {
			return errors.Wrapf(err, "Error parsing configuration")
		}
		fmt.Printf("Error parsing yaml: %v\n", err)
		fmt.Println("Press enter to re-open editor, or ctrl-c to abort")
		_, err := os.Stdin.Read(make([]byte, 1))
		if err != nil {
			return errors.Wrapf(err, "Error reading reply")
		}
		vmbytes, err = shared.TextEditor("", vmbytes)
		if err != nil {
			return errors.Wrapf(err, "Error calling editor")
		}
	}
	if err = EnsureDir(filepath.Dir(cPath)); err != nil {
		return errors.Wrapf(err, "Error creating VM directory")
	}

	if err = os.WriteFile(cPath, vmbytes, 0600); err != nil {
		return errors.Wrapf(err, "Error saving configuration")
	}

	log.Infof("Created VM %s (%s)", cluster, cPath)
	return nil
}
