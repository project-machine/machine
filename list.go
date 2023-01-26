package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var listCmd = cli.Command{
	Name:   "list",
	Usage:  "list defined machines",
	Action: doList,
}

func GetVMInfo(vmPath string) (string, string, error) {
	contents, err := os.ReadFile(vmPath)
	if err != nil {
		return "", "", err
	}
	var vmdef VMDef
	err = yaml.Unmarshal(contents, &vmdef)
	if err != nil {
		return "", "", err
	}
	return vmdef.Name, "unknown", nil
}

func doList(ctx *cli.Context) error {
	lDir := filepath.Join(configDir, "machine")
	ents, err := os.ReadDir(lDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		yamlPath := filepath.Join(lDir, e.Name(), "machine.yaml")
		if !PathExists(yamlPath) {
			continue
		}
		fmt.Printf("%s: (TBD)\n", e.Name())
	}
	return nil
}
