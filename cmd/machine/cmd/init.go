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
	"fmt"
	"io/ioutil"
	"machine/pkg/api"
	"os"
	"path/filepath"
	"sort"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/termios"
	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v2"
)

const machineTypeInsecureVerson1 = `
type: kvm
ephemeral: false
config:
  cpus: 2
  memory: 2048
  uefi: true
  tpm: true
  tpm-version: 2.0
  secure-boot: false
  gui: true
  disks:
    - file: rootdisk.qcow2
      format: qcow2
      type: ssd
      attach: virtio
      bootindex: 0
      size: 50GiB
  nics:
    - id: nic0
      device: virtio-net
      network: user
`

const machineTypeVersion1 = `
type: kvm
ephemeral: false
config:
  cpus: 2
  memory: 2048
  uefi: true
  tpm: true
  tpm-version: 2.0
  secure-boot: true
  gui: true
  disks:
    - file: rootdisk.qcow2
      format: qcow2
      type: ssd
      attach: virtio
      bootindex: 0
      size: 50GiB
`

const defaultMachineType = "1.0"

var machineTypes = map[string]string{
	defaultMachineType: machineTypeVersion1,
	"1.0-insecure":     machineTypeInsecureVerson1,
}

func getMachineTypes() []string {
	var mTypes []string
	for key := range machineTypes {
		mTypes = append(mTypes, key)
	}
	sort.Strings(mTypes)
	return mTypes
}

func getMachineTypeYaml(mType string) (string, error) {
	machine, ok := machineTypes[mType]
	if !ok {
		return "", fmt.Errorf("Unknown machine type '%s'", mType)
	}
	return machine, nil
}

func dataOnStdin() bool {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return true
	}
	return false
}

func DoCreateMachine(machineName, machineType, fileName string, editFile bool) error {
	log.Debugf("DoCreateMachine Name:%s Type:%s File:%s Edit:%v", machineName, machineType, fileName, editFile)
	var err error
	onTerm := termios.IsTerminal(unix.Stdin)
	machine, err := getMachineTypeYaml(machineType)
	if err != nil {
		return fmt.Errorf("Failed to machine type '%s' template: %s", machineType, err)
	}
	machineBytes := []byte(machine)
	newMachine := api.Machine{}

	err = yaml.Unmarshal(machineBytes, &newMachine)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal default machine config: %s", err)
	}
	newMachine.Name = machineName
	newMachine.Config.Name = machineName
	for idx, nic := range newMachine.Config.Nics {
		newMac, err := api.RandomQemuMAC()
		if err != nil {
			return fmt.Errorf("Failed to generate a random QEMU MAC address: %s", err)
		}
		nic.Mac = newMac
		newMachine.Config.Nics[idx] = nic
	}

	log.Infof("Creating machine...")

	// check if edit is set whether we're a terminal or not
	// if file, read contents, else read from stdin
	// launch editor with contents
	// post-edit attempt to marshal contents into Machine definition, retry on failure
	// If machine.Persistent is set, then write contents to config dir, else call api.AddMachine()

	if editFile && !onTerm {
		return fmt.Errorf("Aborting edit since stdin is not a terminal")
	}

	if fileName == "-" || dataOnStdin() {
		log.Info("Reading machine config from stdin...")
		machineBytes, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("Error reading machine definition from stdin: %s", err)
		}
	} else {
		if len(fileName) > 0 {
			log.Infof("Reading machine config from %q", fileName)
			machineBytes, err = os.ReadFile(fileName)
			if err != nil {
				return fmt.Errorf("Error reading definition from %s: %s", fileName, err)
			}
		} else {
			log.Infof("No machine config specified. Using defaults from machine type '%s' ...\n", machineType)
			machineBytes, err = yaml.Marshal(newMachine)
			if err != nil {
				return fmt.Errorf("Failed reading empty machine config: %s", err)
			}
		}
	}

	if editFile {
		machineBytes, err = shared.TextEditor("", machineBytes)
		if err != nil {
			return fmt.Errorf("Error calling editor: %s", err)
		}
	}
	log.Debugf("Got config:\n%s", string(machineBytes))

	for {
		if err = yaml.Unmarshal(machineBytes, &newMachine); err == nil {
			break
		}
		if !onTerm {
			return fmt.Errorf("Error parsing configuration: %s", err)
		}
		fmt.Printf("Error parsing yaml: %v\n", err)
		fmt.Println("Press enter to re-open editor, or ctrl-c to abort")
		_, err := os.Stdin.Read(make([]byte, 1))
		if err != nil {
			return fmt.Errorf("Error reading reply: %s", err)
		}
		machineBytes, err = shared.TextEditor("", machineBytes)
		if err != nil {
			fmt.Errorf("Error calling editor: %s", err)
		}
	}

	if err := checkMachineFilePaths(&newMachine); err != nil {
		return fmt.Errorf("Error while checking machine fiel paths: %s", err)
	}

	// persist config if not ephemeral
	err = postMachine(newMachine)
	if err != nil {
		return fmt.Errorf("Error while POST'ing new machine config: %s", err)
	}
	return nil
}

func verifyPath(base, path string) (string, error) {
	fullPath := path
	if strings.HasPrefix(path, "/") {
		fullPath = path
	} else if strings.HasPrefix(fullPath, "~/") {
		ePath, err := homedir.Expand(fullPath)
		if err != nil {
			return "", fmt.Errorf("Failed to expand '~/' in path string %q: %s", fullPath, err)
		}
		log.Infof("Expanded %s to %q", fullPath, ePath)
		fullPath = ePath
	} else {
		fullPath = filepath.Join(base, path)
	}

	if !api.PathExists(fullPath) {
		return "", fmt.Errorf("Failed to find specified file '%s'", fullPath)
	}

	return fullPath, nil
}

func checkMachineFilePaths(newMachine *api.Machine) error {
	log.Infof("Checking machine definition for local file paths...")
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("Failed to get current working dir: %s", err)
	}
	for idx := range newMachine.Config.Disks {
		disk := newMachine.Config.Disks[idx]
		// skip disks to be created (file does not exist but size > 0)
		if disk.File != "" && disk.Size == 0 {
			newPath, err := verifyPath(cwd, disk.File)
			if err != nil {
				return fmt.Errorf("Failed to verify path to disk %q", disk.File)
			}
			if newPath != disk.File {
				log.Infof("Fully qualified disk path %s", newPath)
				disk.File = newPath
				newMachine.Config.Disks[idx] = disk
			}
		}
	}
	if newMachine.Config.Cdrom != "" {
		newPath, err := verifyPath(cwd, newMachine.Config.Cdrom)
		if err != nil {
			return fmt.Errorf("Failed to verify path to cdrom %q", newMachine.Config.Cdrom)
		}
		log.Infof("Fully qualified cdrom path %s", newPath)
		newMachine.Config.Cdrom = newPath
	}
	if newMachine.Config.UEFIVars != "" {
		newPath, err := verifyPath(cwd, newMachine.Config.UEFIVars)
		if err != nil {
			return fmt.Errorf("Failed to verify path to uefi-vars: %q: %s", newMachine.Config.UEFIVars, err)
		}
		log.Infof("Fully qualified uefi-vars path %s", newPath)
		newMachine.Config.UEFIVars = newPath
	}
	if newMachine.Config.UEFICode != "" {
		newPath, err := verifyPath(cwd, newMachine.Config.UEFICode)
		if err != nil {
			return fmt.Errorf("Failed to verify path to uefi-code: %q: %s", newMachine.Config.UEFICode, err)
		}
		log.Infof("Fully qualified uefi-code path %s", newPath)
		newMachine.Config.UEFICode = newPath
	}
	return nil
}

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:               "init <machine name>",
	Short:             "Initialize a new machine from yaml",
	Long:              `Initilize a new machine by specifying a machine yaml configuring.`,
	RunE:              doInit,
	PersistentPreRunE: doInitArgsValidate,
}

func doInit(cmd *cobra.Command, args []string) error {
	fileName := cmd.Flag("file").Value.String()
	// Hi cobra, this is awkward...  why isn't there .Value.Bool()?
	editFile, _ := cmd.Flags().GetBool("edit")
	var machineName string
	if len(args) > 0 {
		machineName = args[0]
	} else {
		machineName = petname.Generate(petNameWords, petNameSep)
	}
	machineType := cmd.Flag("machine-type").Value.String()

	if err := DoCreateMachine(machineName, machineType, fileName, editFile); err != nil {
		return fmt.Errorf("Failed to create machine with type '%s': %s", machineType, err)
	}
	return nil
}

func doInitArgsValidate(cmd *cobra.Command, args []string) error {
	mType := cmd.Flag("machine-type").Value.String()
	if _, ok := machineTypes[mType]; !ok {
		mTypes := getMachineTypes()
		cmd.SilenceUsage = true
		return fmt.Errorf("Invalid machine-type '%s', must be one of: %s", mType, strings.Join(mTypes, ", "))
	}
	debug, _ := cmd.Flags().GetBool("debug")
	if debug {
		log.SetLevel(log.DebugLevel)
	}
	return nil
}

func init() {
	mTypes := getMachineTypes()
	rootCmd.AddCommand(initCmd)
	initCmd.PersistentFlags().StringP("file", "f", "", "yaml file to import.  If unspecified, use stdin")
	initCmd.PersistentFlags().BoolP("edit", "e", false, "edit the yaml file inline")
	initCmd.PersistentFlags().BoolP("debug", "D", false, "enable debug logging")
	initCmd.PersistentFlags().StringP("machine-type", "m", defaultMachineType, fmt.Sprintf("specify the machine type, one of [%s]", strings.Join(mTypes, ", ")))
}
