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
	"fmt"
	"os"

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tilegroxy",
	Short: "A service to proxy and cache map tile layers",
	Long: `Tilegroxy is an extensible CLI application that proxies mapping layers to external providers and adds cacheing and protection in front. 

	Tilegroxy is meant to be used to power "ZXY" tile layers commonly used in web mapping applications and only provides endpoints in this scheme.  
	However one use of tilegroxy is as an adapter to convert other mapping APIs such as WMS to a simple tile layer. Any API that returns georeferenced
	imagery can be used with tilegroxy.
	
	See the documentation at TODO for configuration instructions.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		exit(1)
	}
}

var exitStatus int = -1

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

// A common utility for use by multiple commands to bootstrap the core application entities
func parseConfigIntoStructs(cmd *cobra.Command) (*config.Config, *layers.LayerGroup, authentication.Authentication, error) {
	var err error
	configPath, err1 := cmd.Flags().GetString("config")
	configRaw, err2 := cmd.Flags().GetString("raw-config")
	remoteProvider, err3 := cmd.Flags().GetString("remote-provider")
	remoteEndpoint, err4 := cmd.Flags().GetString("remote-endpoint")
	remotePath, err5 := cmd.Flags().GetString("remote-path")
	remoteType, err6 := cmd.Flags().GetString("remote-type")

	if err = errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
		return nil, nil, nil, err
	}

	var cfg config.Config

	if configRaw != "" {
		cfg, err = config.LoadConfig(configRaw)
	} else if remoteProvider != "" {
		cfg, err = config.LoadConfigFromRemote(remoteProvider, remoteEndpoint, remotePath, remoteType)
	} else if configPath != "" {
		cfg, err = config.LoadConfigFromFile(configPath)
	} else {
		return nil, nil, nil, errors.New("no configuration supplied")
	}

	if err != nil {
		return nil, nil, nil, err
	}

	cache, err := caches.ConstructCache(cfg.Cache, &cfg.Error.Messages)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error constructing cache: %v", err)
	}

	auth, err := authentication.ConstructAuth(cfg.Authentication, &cfg.Error.Messages)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error constructing auth: %v", err)
	}

	layerGroup, err := layers.ConstructLayerGroup(cfg, cfg.Layers, cache)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error constructing layers: %v", err)
	}

	return &cfg, layerGroup, auth, err
}
