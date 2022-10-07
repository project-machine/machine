package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var listCmd = cli.Command{
	Name: "list",
	Usage: "list defined machines",
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
	lDir := filepath.Join(configDir, "machine", envDir)
	ents, err := os.ReadDir(lDir)
	if err != nil {
		return err
	}
	errlist := []string{}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".yaml") {
			vmPath := filepath.Join(lDir, e.Name())
			name, status, err := GetVMInfo(vmPath)
			if err != nil {
				n := fmt.Sprintf("Error reading %s: %v", vmPath, err)
				errlist = append(errlist, n)
				continue
			}
			fmt.Printf("%s: %s\n", name, status)
		}
	}
	if len(errlist) != 0 {
		fmt.Printf("Errors encountered:\n%s\n", strings.Join(errlist, "\n"))
	}
	return nil
}
