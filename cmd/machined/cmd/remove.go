package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/project-machine/machine/pkg/api"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// removeCmd represents the remove command
var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove systemd --user unit files",
	Long:  `Remove systemd unit files for machined service with socket activation.`,
	RunE:  doRemove,
}

func doRemove(cmd *cobra.Command, args []string) error {
	hostMode, _ := cmd.Flags().GetBool("host")
	unitPath, err := getSystemdUnitPath(hostMode)
	if err != nil {
		return fmt.Errorf("Failed to get Systemd Unit Path: %s", err)
	}
	serviceUnit := filepath.Join(unitPath, MachinedServiceUnit)
	socketUnit := filepath.Join(unitPath, MachinedSocketUnit)
	removed := false
	if api.PathExists(serviceUnit) {
		log.Infof("Removing unit %s", serviceUnit)
		if err := os.Remove(serviceUnit); err != nil {
			return fmt.Errorf("Failed to remove %q: %s", serviceUnit, err)
		}
		removed = true
		runCmd := exec.Command("systemctl", "--user", "stop", MachinedServiceUnit)
		out, err := runCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Failed to stop unit %s: %s: %s", MachinedServiceUnit, string(out), err)
		}
	}
	if api.PathExists(socketUnit) {
		log.Infof("Removing unit %s", socketUnit)
		if err := os.Remove(socketUnit); err != nil {
			return fmt.Errorf("Failed to remove %q: %s", socketUnit, err)
		}
		removed = true
		log.Infof("Stopping unit %s", socketUnit)
		runCmd := exec.Command("systemctl", "--user", "stop", MachinedSocketUnit)
		out, err := runCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Failed to stop unit %s: %s: %s", MachinedSocketUnit, string(out), err)
		}
	}
	if removed {
		args := []string{"daemon-reload"}
		if !hostMode {
			args = append(args, "--user")
		}
		log.Infof("Reloading systemd units")
		runCmd := exec.Command("systemctl", args...)
		_, err := runCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Failed to reload units: %s", err)
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(removeCmd)
	removeCmd.PersistentFlags().BoolP("host", "H", false, "remove systemd units in /etc/systemd/system instead of systemd --user path")
}
