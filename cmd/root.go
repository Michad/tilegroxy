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
	Long: `Tilegroxy is an extensible CLI application that proxies mapping layers to external providers and adds caching and protections in front. 

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

type configFlags struct {
	path           string
	raw            string
	remoteProvider string
	remoteEndpoint string
	remotePath     string
	remoteType     string
	reload         bool
}

func readConfigFlags(cmd *cobra.Command) (configFlags, error) {
	var f configFlags
	var err1, err2, err3, err4, err5, err6 error

	f.path, err1 = cmd.Flags().GetString("config")
	f.raw, err2 = cmd.Flags().GetString("raw-config")
	f.remoteProvider, err3 = cmd.Flags().GetString("remote-provider")
	f.remoteEndpoint, err4 = cmd.Flags().GetString("remote-endpoint")
	f.remotePath, err5 = cmd.Flags().GetString("remote-path")
	f.remoteType, err6 = cmd.Flags().GetString("remote-type")

	// Only defined in the serve command, so an error just means hot reloading isn't supported
	f.reload, _ = cmd.Flags().GetBool(reloadFlag)

	return f, errors.Join(err1, err2, err3, err4, err5, err6)
}

// A common utility for use by multiple commands to bootstrap the core application config.
// reloadFunc is optional if hot reloading is not supported in triggering command
func extractConfigFromCommand(cmd *cobra.Command, reloadFunc func(c config.Config, err error)) (*config.Config, error) {
	f, err := readConfigFlags(cmd)
	if err != nil {
		return nil, err
	}

	var cfg config.Config

	switch {
	case f.raw != "":
		cfg, err = config.LoadConfig(f.raw)
	case f.remoteProvider != "":
		cfg, err = config.LoadConfigFromRemote(f.remoteProvider, f.remoteEndpoint, f.remotePath, f.remoteType)
	case f.reload && reloadFunc != nil:
		cfg, err = config.LoadAndWatchConfigFromFile(f.path, reloadFunc)
	case f.path != "":
		cfg, err = config.LoadConfigFromFile(f.path)
	default:
		err = errors.New("no configuration supplied")
	}

	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Re-reads the configuration from wherever the command loaded it
func reloadSourceFromCommand(cmd *cobra.Command) (func() (config.Config, error), error) {
	f, err := readConfigFlags(cmd)
	if err != nil {
		return nil, err
	}

	if f.raw != "" {
		return func() (config.Config, error) {
			return config.LoadConfig(f.raw)
		}, nil
	}

	if f.remoteProvider != "" {
		return func() (config.Config, error) {
			return config.LoadConfigFromRemote(f.remoteProvider, f.remoteEndpoint, f.remotePath, f.remoteType)
		}, nil
	}

	return func() (config.Config, error) {
		return config.LoadConfigFromFile(f.path)
	}, nil
}
