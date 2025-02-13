// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"errors"
	"flag"
	"os"

	_ "github.com/Michad/tilegroxy/internal/authentications"
	_ "github.com/Michad/tilegroxy/internal/caches"
	_ "github.com/Michad/tilegroxy/internal/providers"
	_ "github.com/Michad/tilegroxy/internal/secrets"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/spf13/cobra"
)

const reloadFlag = "hot-reload"

var rootCmd = &cobra.Command{
	Use:   "tilegroxy",
	Short: "A service to proxy and cache map tile layers",
	Long: `Tilegroxy is an extensible CLI application that proxies mapping layers to external providers and adds cacheing and protection in front. 

	Tilegroxy is meant to be used to power "ZXY" tile layers commonly used in web mapping applications and only provides endpoints in this scheme.  
	However one use of tilegroxy is as an adapter to convert other mapping APIs such as WMS to a simple tile layer. Any API that returns georeferenced
	imagery can be used with tilegroxy.
	
	See the documentation at https://github.com/michad/tilegroxy for configuration instructions.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		exit(1)
	}
}

var exitStatus = -1

//nolint:revive
func exit(status int) {
	if flag.Lookup("test.v") == nil {
		os.Exit(status)
	} else {
		exitStatus = status
	}
}

func init() {
	initRoot()
}

func initRoot() {
	rootCmd.PersistentFlags().StringP("config", "c", "./tilegroxy.yml", "A file path to the configuration file to use. The file should have an extension of either json or yml/yaml and be readable.")
	rootCmd.PersistentFlags().String("raw-config", "", "The full configuration to be used as JSON.")
	rootCmd.PersistentFlags().String("remote-provider", "", "The provider to pull configuration from. One of: etcd, etcd3, consul, firestore, nats")
	rootCmd.PersistentFlags().String("remote-endpoint", "http://127.0.0.1:2379", "The endpoint to use to connect to the remote provider")
	rootCmd.PersistentFlags().String("remote-path", "/config/tilegroxy.yml", "The path to use to select the configuration on the remote provider")
	rootCmd.PersistentFlags().String("remote-type", "yaml", "The file format to use to parse the configuration from the remote provider")
	rootCmd.MarkFlagsMutuallyExclusive("config", "raw-config", "remote-provider")
}

// A common utility for use by multiple commands to bootstrap the core application config.
// reloadFunc is optional if hot reloading is not supported in triggering command
func extractConfigFromCommand(cmd *cobra.Command, reloadFunc func(c config.Config, err error)) (*config.Config, error) {
	var err error
	configPath, err1 := cmd.Flags().GetString("config")
	configRaw, err2 := cmd.Flags().GetString("raw-config")
	remoteProvider, err3 := cmd.Flags().GetString("remote-provider")
	remoteEndpoint, err4 := cmd.Flags().GetString("remote-endpoint")
	remotePath, err5 := cmd.Flags().GetString("remote-path")
	remoteType, err6 := cmd.Flags().GetString("remote-type")

	reload, err := cmd.Flags().GetBool(reloadFlag)
	if err != nil {
		// This is only defined in the serve command so expect it to fail in commands that don't support reload
		reload = false
	}

	if err = errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
		return nil, err
	}

	var cfg config.Config

	switch {
	case reload && reloadFunc != nil:
		cfg, err = config.LoadAndWatchConfigFromFile(configPath, reloadFunc)
	case configRaw != "":
		cfg, err = config.LoadConfig(configRaw)
	case remoteProvider != "":
		cfg, err = config.LoadConfigFromRemote(remoteProvider, remoteEndpoint, remotePath, remoteType)
	case configPath != "":
		cfg, err = config.LoadConfigFromFile(configPath)
	default:
		err = errors.New("no configuration supplied")
	}

	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
