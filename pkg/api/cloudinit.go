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
package api

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	NoCloudFSLabel = "cidata"
)

/*
type: kvm
config:

	name: slick-seal
	...
	cloud-init:
	    user-data:|
	        #cloud-config
	        runcmd:
	        - cat /etc/os-release
	    network-config:|
	        version: 2
	        ethernets:
	            nic0:
	                match:
	                    name: en*
	                dhcp4: true
	    meta-data:|
	        instance-id: 08b2083d-2935-4d50-a442-d1da8920de20
	        local-hostname: slick-seal
*/

type CloudInitConfig struct {
	NetworkConfig string `yaml:"network-config"`
	UserData      string `yaml:"user-data"`
	MetaData      string `yaml:"meta-data"`
}

type MetaData struct {
	InstanceId    string `yaml:"instance-id"`
	LocalHostname string `yaml:"local-hostname"`
}

func HasCloudConfig(config CloudInitConfig) bool {

	if config.MetaData != "" {
		return true
	}
	if config.UserData != "" {
		return true
	}
	if config.NetworkConfig != "" {
		return true
	}
	return false
}

func PrepareMetadata(config *CloudInitConfig, hostname string) error {
	// update MetaData with local-hostname and instance-id if not set

	if config.MetaData != "" {
		return fmt.Errorf("cloud-init config has existing metadata")
	}

	iid := uuid.New()

	md := MetaData{
		InstanceId:    iid.String(),
		LocalHostname: hostname,
	}

	content, err := yaml.Marshal(&md)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %s", err)
	}

	config.MetaData = string(content)

	return nil
}

func RenderCloudInitConfig(config CloudInitConfig, outputPath string) error {

	renderedFiles := 0
	for _, d := range []struct {
		confFile string
		confData string
	}{
		{
			confFile: "network-config",
			confData: config.NetworkConfig,
		},
		{
			confFile: "user-data",
			confData: config.UserData,
		},
		{
			confFile: "meta-data",
			confData: config.MetaData,
		},
	} {
		if len(d.confData) > 0 {
			configFile := filepath.Join(outputPath, d.confFile)
			tempFile, err := os.CreateTemp("", "tmp-cloudinit-")
			if err != nil {
				return fmt.Errorf("failed to create a temp file for writing cloud-init %s file: %s", d.confFile, err)
			}
			defer tempFile.Close()
			defer os.Remove(tempFile.Name())
			if err := os.WriteFile(tempFile.Name(), []byte(d.confData), 0666); err != nil {
				return fmt.Errorf("failed to write cloud-init %s file %q: %s", d.confFile, tempFile.Name(), err)
			}
			if err := os.Rename(tempFile.Name(), configFile); err != nil {
				return fmt.Errorf("failed to rename temp file %q to %q: %s", tempFile.Name(), configFile, err)
			}
			renderedFiles++
		}
	}
	if renderedFiles == 0 {
		return fmt.Errorf("failed to render any cloud-init config files; maybe empty cloud-init config?")
	}
	return nil
}

func verifyCloudInitConfig(cfg CloudInitConfig, contentsDir string) error {

	// read the extracted directory and validate CloudInitConfig files
	err := filepath.Walk(contentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			log.Infof("verifyCloudInitCfg: path:%s name:%s contents:%s", path, info.Name(), contents)
			switch info.Name() {
			case "network-config":
				if cfg.NetworkConfig != string(contents) {
					return fmt.Errorf("network-config: expected contents %q, got %q", cfg.NetworkConfig, string(contents))
				}
			case "user-data":
				if cfg.UserData != string(contents) {
					return fmt.Errorf("user-data: expected contents %q, got %q", cfg.UserData, string(contents))
				}
			case "meta-data":
				if cfg.MetaData != string(contents) {
					return fmt.Errorf("meta-data: expected contents %q, got %q", cfg.MetaData, string(contents))
				}
			default:
				return fmt.Errorf("Unexpected file %q in cloud-init rendered directory", info.Name())
			}
		} else {
			if info.Name() != filepath.Base(path) {
				return fmt.Errorf("Unexpected directory %q in cloud-init rendered directory", info.Name())
			}
		}

		return nil
	})

	return err
}

func CreateLocalDataSource(cfg CloudInitConfig, directory string) error {

	if err := EnsureDir(directory); err != nil {
		return fmt.Errorf("failed to create cloud-init data source directory %q: %s", directory, err)
	}

	if err := RenderCloudInitConfig(cfg, directory); err != nil {
		return fmt.Errorf("failed to render cloud-init config to directory %q: %s", directory, err)
	}

	if err := verifyCloudInitConfig(cfg, directory); err != nil {
		return fmt.Errorf("failed to verify cloud-init config content in directory %q: %s", directory, err)
	}

	return nil
}
