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

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install systemd --user unit files",
	Long:  `Install systemd unit files for machined service with socket activation.`,
	RunE:  doInstall,
}

func doInstall(cmd *cobra.Command, args []string) error {
	hostMode, _ := cmd.Flags().GetBool("host")
	unitPath, err := getSystemdUnitPath(hostMode)
	if err != nil {
		return fmt.Errorf("Failed to get Systemd Unit Path: %s", err)
	}
	if !api.PathExists(unitPath) {
		if err := api.EnsureDir(unitPath); err != nil {
			return fmt.Errorf("Failed to create Systemd Unit path %q: %s", unitPath, err)
		}
	}
	serviceUnit := filepath.Join(unitPath, MachinedServiceUnit)
	socketUnit := filepath.Join(unitPath, MachinedSocketUnit)
	customService := cmd.Flag("service-template").Value.String()
	serviceTemplate := getTemplate(customService, MachinedServiceTemplate)
	customSocket := cmd.Flag("socket-template").Value.String()
	socketTemplate := getTemplate(customSocket, MachinedSocketTemplate)

	// check if files exist and exit asking for --force flag
	overwrite, _ := cmd.Flags().GetBool("force")
	if api.PathExists(serviceUnit) && api.PathExists(socketUnit) {
		log.Infof("machined service and socket units already exist: %q, %q", serviceUnit, socketUnit)
		if !overwrite {
			return nil
		}
		log.Infof("--force specified, overwriting files")
	}
	log.Infof("machined missing service and/or socket unit(s), installing..")
	if !api.PathExists(serviceUnit) {
		if err := installTemplate(serviceTemplate, serviceUnit); err != nil {
			return fmt.Errorf("Failed to render template to %q: %s", serviceUnit, err)
		}
	}
	if !api.PathExists(socketUnit) {
		if err := installTemplate(socketTemplate, socketUnit); err != nil {
			return fmt.Errorf("Failed to render service to %q: %s", socketUnit, err)
		}
	}

	runCmd := exec.Command("systemctl", "--user", "daemon-reload")
	out, err := runCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to 'daemon-reload' systemd --user: %s: %s", string(out), err)
	}

	runCmd = exec.Command("systemctl", "--user", "start", MachinedSocketUnit)
	out, err = runCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to start unit %s: %s: %s", MachinedSocketUnit, string(out), err)
	}

	log.Infof("Checking machined.socket status...")
	runCmd = exec.Command("systemctl", "--no-pager", "--user", "status", MachinedSocketUnit)
	runCmd.Stdout = os.Stdout
	err = runCmd.Run()
	if err != nil {
		return fmt.Errorf("Failed to start unit %s: %s: %s", MachinedSocketUnit, string(out), err)
	}
	log.Infof("Useful systemctl commands:")
	log.Infof("")
	log.Infof("  systemctl --user status %s", MachinedSocketUnit)
	log.Infof("  systemctl --user status %s", MachinedServiceUnit)
	log.Infof("")
	log.Infof("To run an updated machined binary, run:")
	log.Infof("")
	log.Infof("  systemctl --user stop %s", MachinedServiceUnit)
	return nil
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.PersistentFlags().BoolP("host", "H", false, "install systemd units to /etc/systemd/system instead of systemd --user path")
	installCmd.PersistentFlags().BoolP("force", "f", false, "allow overwriting existing unit files when installing")
	installCmd.PersistentFlags().StringP("service-template", "s", "", "specify path to custom machined service template")
	installCmd.PersistentFlags().StringP("socket-template", "S", "", "specify path to custom machined socket template")
}
