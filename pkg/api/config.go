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
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type MachineDaemonConfig struct {
	ConfigDirectory string
	DataDirectory   string
	StateDirectory  string
}

var (
	mdcCtx         = "mdc-context"
	mdcCtxConfDir  = mdcCtx + "-confdir"
	mdcCtxDataDir  = mdcCtx + "-datadir"
	mdcCtxStateDir = mdcCtx + "-statedir"
)

func DefaultMachineDaemonConfig() *MachineDaemonConfig {
	cfg := MachineDaemonConfig{}
	udd, err := UserDataDir()
	if err != nil {
		panic(fmt.Sprintf("Error getting user data dir: %s", err))
	}
	ucd, err := UserConfigDir()
	if err != nil {
		panic(fmt.Sprintf("Error getting user config dir: %s", err))
	}
	usd, err := UserStateDir()
	if err != nil {
		panic(fmt.Sprintf("Error getting user state dir: %s", err))
	}
	cfg.ConfigDirectory = filepath.Join(ucd, "machine")
	cfg.DataDirectory = filepath.Join(udd, "machine")
	cfg.StateDirectory = filepath.Join(usd, "machine")
	return &cfg
}

func (c *MachineDaemonConfig) GetConfigContext() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, mdcCtxConfDir, c.ConfigDirectory)
	ctx = context.WithValue(ctx, mdcCtxDataDir, c.DataDirectory)
	ctx = context.WithValue(ctx, mdcCtxStateDir, c.StateDirectory)
	return ctx
}

// FIXME: read from ENV variables before using fallback values
//  XDG_DATA_HOME
func UserDataDir() (string, error) {
	p, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, ".local", "share"), nil
}

// FIXME: read from ENV variables before using fallback values
//  XDG_CONFIG_HOME
func UserConfigDir() (string, error) {
	p, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, ".config"), nil
}

// FIXME: read from ENV variables before using fallback values
//  XDG_STATE_HOME
func UserStateDir() (string, error) {
	p, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, ".local", "state"), nil
}
