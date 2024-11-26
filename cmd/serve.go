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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Michad/tilegroxy/pkg/config"
	tg "github.com/Michad/tilegroxy/pkg/entry"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Starts a long-running process to service HTTP tile requests",
	Long: `Creates an HTTP server that listens for requests for tiles and serves them up in accordance with the configuration.

	The process will run blocking in the foreground until terminated. The majority of configuration should be supplied via a configuration file.
	
	`,
	Run: runServe,
}

const reloadFlag = "hot-reload"

func runServe(cmd *cobra.Command, _ []string) {
	out := rootCmd.OutOrStdout()

	reload, err := cmd.Flags().GetBool(reloadFlag)
	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err.Error())
		exit(1)
		return
	}

	var reloadPtr func(*config.Config) error

	if reload {
		fmt.Fprintln(out, "Hot-loading is enabled")
		configPath, err := cmd.Flags().GetString("config")
		if err != nil {
			fmt.Fprintf(out, "Error: %v\n", err.Error())
			exit(1)
			return
		}

		var cfgPrime *config.Config

		cfg, err := config.LoadAndWatchConfigFromFile(configPath, func(c config.Config, err error) {
			if err != nil {
				fmt.Fprintf(out, "Error: %v\n", err.Error())
			} else if reloadPtr != nil {
				err := (reloadPtr)(&c)
				if err != nil {
					fmt.Fprintf(out, "Error: %v\n", err.Error())
				}
			}
		})

		cfgPrime = &cfg

		if err != nil {
			fmt.Fprintf(out, "Error: %v\n", err.Error())
			exit(1)
			return
		}

		err = tg.Serve(cfgPrime, tg.ServeOptions{}, out, &reloadPtr)

		if err != nil {
			fmt.Fprintf(out, "Error: %v\n", err.Error())
			exit(1)
			return
		}

	} else {
		cfg, err := extractConfigFromCommand(cmd)
		if err != nil {
			fmt.Fprintf(out, "Error: %v\n", err.Error())
			exit(1)
			return
		}

		err = tg.Serve(cfg, tg.ServeOptions{}, out, &reloadPtr)

		if err != nil {
			fmt.Fprintf(out, "Error: %v\n", err.Error())
			exit(1)
			return
		}
	}
}

func init() {
	initServe()
}

func initServe() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().Bool(reloadFlag, false, "Automatically reload configuration upon changes")
	serveCmd.MarkFlagsMutuallyExclusive("raw-config", reloadFlag)
	serveCmd.MarkFlagsMutuallyExclusive("remote-provider", reloadFlag)
	serveCmd.MarkFlagsMutuallyExclusive("remote-endpoint", reloadFlag)
	serveCmd.MarkFlagsMutuallyExclusive("remote-path", reloadFlag)
	serveCmd.MarkFlagsMutuallyExclusive("remote-type", reloadFlag)
}
