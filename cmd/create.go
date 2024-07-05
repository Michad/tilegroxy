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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates a bare-bones configuration",
	Long: `Creates either a JSON or YAML configuration with a skeleton you can use as a starting point for creating your configuration. 
	
Defaults to outputting to standard out, specify --output/-o to write to a file. Does not utilize --config/-c to avoid accidentally overwriting a configuration. If a file is specified this defaults to auto-detecting the format to use based on the file extension and ultimately defaults to YAML.
	
Example:
	tilegroxy config create --default --json -o tilegroxy.json`,
	Run: runCreate,
}

func runCreate(cmd *cobra.Command, args []string) {
	includeDefault, _ := cmd.Flags().GetBool("default")
	noPretty, _ := cmd.Flags().GetBool("no-pretty")
	forceJson, _ := cmd.Flags().GetBool("json")
	forceYml, _ := cmd.Flags().GetBool("yaml")
	writePath, _ := cmd.Flags().GetString("output")

	out := cmd.OutOrStdout()

	cfg := make(map[string]interface{})

	if includeDefault {
		mapstructure.Decode(config.DefaultConfig(), &cfg)
	}

	if writePath != "" && !forceJson && !forceYml {
		ext := strings.ToLower(filepath.Ext(writePath))

		if ext == ".json" {
			forceJson = true
		} //Check for extension being yaml isn't needed because we default to yaml
	}

	//TODO: populate example config here

	var file *os.File
	var err error

	if writePath != "" {
		file, err = os.OpenFile(writePath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0666)

		if err != nil {
			panic(err)
		}

		defer file.Close()
	}

	if forceJson {
		var enc *json.Encoder

		if writePath != "" {
			enc = json.NewEncoder(file)
		} else {
			enc = json.NewEncoder(out)
		}
		if !noPretty {
			enc.SetIndent(" ", "  ")
		}
		enc.Encode(cfg)
	} else {
		var enc *yaml.Encoder

		if writePath != "" {
			enc = yaml.NewEncoder(file)
		} else {
			enc = yaml.NewEncoder(out)
		}
		enc.Encode(cfg)
	}
}

func init() {
	initCreate()
}

func initCreate() {
	configCmd.AddCommand(createCmd)

	createCmd.Flags().BoolP("default", "d", true, "Include all default configuration. TODO: make this non-mandatory")

	createCmd.Flags().Bool("json", false, "Output the configuration in JSON")
	createCmd.Flags().Bool("yaml", false, "Output the configuration in YAML")
	createCmd.MarkFlagsMutuallyExclusive("json", "yaml")

	createCmd.Flags().Bool("no-pretty", false, "Disable pretty printing JSON")
	createCmd.MarkFlagsMutuallyExclusive("no-pretty", "yaml")

	createCmd.Flags().StringP("output", "o", "", "Write the configuration to a file. This will overwrite anything already in the file")
}
