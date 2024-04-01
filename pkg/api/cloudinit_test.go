package api

import (
	"os"
	"testing"
)

func TestCloudInitRendersConfig(t *testing.T) {

	cfg := CloudInitConfig{
		NetworkConfig: "network-config",
		UserData:      "user-data",
		MetaData:      "meta-data",
	}

	tmpDir, err := os.MkdirTemp("", "test-ci-render-config")
	if err != nil {
		t.Fatalf("failed to create a tempdir for test")
	}
	defer os.RemoveAll(tmpDir)

	err = RenderCloudInitConfig(cfg, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error when rendering cloud-init config: %s", err)
	}

	err = verifyCloudInitConfig(cfg, tmpDir)
	if err != nil {
		t.Fatalf("failed to verify rendered contents: %s", err)
	}
}

func TestCloudInitRenderConfigFailsOnEmpty(t *testing.T) {

	cfg := CloudInitConfig{
		NetworkConfig: "",
		UserData:      "",
		MetaData:      "",
	}

	tmpDir, err := os.MkdirTemp("", "test-ci-render-config")
	if err != nil {
		t.Fatalf("failed to create a tempdir for test")
	}
	defer os.RemoveAll(tmpDir)

	err = RenderCloudInitConfig(cfg, tmpDir)
	if err == nil {
		t.Fatalf("expected empty config to return an error, got nil instead")
	}
}

func TestPrepareMetadataUpdatesConfig(t *testing.T) {
	vmCfg := VMDef{
		Name: "myVM1",
		CloudInit: CloudInitConfig{
			NetworkConfig: "network-config",
			UserData:      "user-data",
			MetaData:      "",
		},
	}

	err := PrepareMetadata(&vmCfg.CloudInit, vmCfg.Name)
	if err != nil {
		t.Fatalf("failed to prepare metadata: %s", err)
	}

	// log.Infof("vmCfg: %+v", vmCfg)
	if vmCfg.CloudInit.MetaData == "" {
		t.Fatalf("failed to update metadata, it's empty")
	}

	tmpDir, err := os.MkdirTemp("", "test-ci-render-config")
	if err != nil {
		t.Fatalf("failed to create a tempdir for test")
	}
	defer os.RemoveAll(tmpDir)

	err = RenderCloudInitConfig(vmCfg.CloudInit, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error when rendering cloud-init config: %s", err)
	}
}

func TestCloudInitCreatesDataSource(t *testing.T) {

	cfg := CloudInitConfig{
		NetworkConfig: "network-config",
		UserData:      "user-data",
		MetaData:      "meta-data",
	}

	seedDir, err := os.MkdirTemp("", "test-ci-seed")
	if err != nil {
		t.Fatalf("failed to create a tempdir for test")
	}
	defer os.RemoveAll(seedDir)

	err = CreateLocalDataSource(cfg, seedDir)
	if err != nil {
		t.Fatalf("failed to cloud-init datasource: %s", err)
	}

}

func TestPrepareMetadataUpdatesPresentInDataSource(t *testing.T) {
	vmCfg := VMDef{
		Name: "myVM1",
		CloudInit: CloudInitConfig{
			NetworkConfig: "network-config",
			UserData:      "user-data",
			MetaData:      "",
		},
	}

	err := PrepareMetadata(&vmCfg.CloudInit, vmCfg.Name)
	if err != nil {
		t.Fatalf("failed to prepare metadata: %s", err)
	}

	seedDir, err := os.MkdirTemp("", "test-ci-seed")
	if err != nil {
		t.Fatalf("failed to create a tempdir for test")
	}
	defer os.RemoveAll(seedDir)

	// log.Infof("vmCfg: %+v", vmCfg)
	if vmCfg.CloudInit.MetaData == "" {
		t.Fatalf("failed to update metadata, it's empty")
	}

	err = CreateLocalDataSource(vmCfg.CloudInit, seedDir)
	if err != nil {
		t.Fatalf("failed to create data source: %s", err)
	}
}
