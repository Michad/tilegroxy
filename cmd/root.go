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
	rootCmd.PersistentFlags().String("raw-config", "", "The full configuration to be used.")
	rootCmd.MarkFlagsMutuallyExclusive("config", "raw-config")
}

// A common utility for use by multiple commands to bootstrap the core application entities
func parseConfigIntoStructs(cmd *cobra.Command) (*config.Config, []*layers.Layer, *authentication.Authentication, error) {
	var err error
	configPath, err1 := cmd.Flags().GetString("config")
	configRaw, err2 := cmd.Flags().GetString("raw-config")

	if err = errors.Join(err1, err2); err != nil {
		return nil, nil, nil, err
	}

	var cfg config.Config

	if configRaw != "" {
		cfg, err = config.LoadConfig(configRaw)
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

	layerObjects := make([]*layers.Layer, len(cfg.Layers))

	for i, l := range cfg.Layers {
		layerObjects[i], err = layers.ConstructLayer(l, &cfg.Client, &cfg.Error.Messages)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error constructing layer %v: %v", i, err)
		}

		layerObjects[i].Cache = &cache
	}

	return &cfg, layerObjects, &auth, err
}
