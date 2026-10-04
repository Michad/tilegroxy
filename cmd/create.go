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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Outputs the default configuration",
	Long: `Creates either a JSON or YAML configuration with a skeleton containing all default values. 
	
Defaults to outputting to standard out, specify --output/-o to write to a file. Does not utilize --config/-c to avoid accidentally overwriting a configuration. If a file is specified this defaults to auto-detecting the format to use based on the file extension and ultimately defaults to YAML.
	
Example:
	tilegroxy config create --json -o tilegroxy.json`,
	Run: runCreate,
}

func runCreate(cmd *cobra.Command, _ []string) {
	if err := createConfig(cmd); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
		exit(1)
	}
}

func createConfig(cmd *cobra.Command) error {
	noPretty, _ := cmd.Flags().GetBool("no-pretty")
	forceJSON, _ := cmd.Flags().GetBool("json")
	forceYML, _ := cmd.Flags().GetBool("yaml")
	writePath, _ := cmd.Flags().GetString("output")

	cfg := make(map[string]interface{})
	if err := mapstructure.Decode(config.DefaultConfig(), &cfg); err != nil {
		return err
	}

	if writePath != "" && !forceJSON && !forceYML {
		ext := strings.ToLower(filepath.Ext(writePath))

		if ext == ".json" {
			forceJSON = true
		} // Check for extension being yaml isn't needed because we default to yaml
	}

	if writePath == "" {
		return encodeConfig(cmd.OutOrStdout(), cfg, forceJSON, !noPretty)
	}

	file, err := os.OpenFile(filepath.Clean(writePath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if file != nil {
		defer file.Close()
	}
	if err != nil {
		return err
	}

	return encodeConfig(file, cfg, forceJSON, !noPretty)
}

func encodeConfig(out io.Writer, cfg map[string]interface{}, forceJSON bool, prettyIfJSON bool) error {
	if forceJSON {
		enc := json.NewEncoder(out)
		if prettyIfJSON {
			enc.SetIndent(" ", "  ")
		}

		return enc.Encode(cfg)
	}

	enc := yaml.NewEncoder(out)
	defer enc.Close()

	return enc.Encode(cfg)
}

func init() {
	initCreate()
}

func initCreate() {
	configCmd.AddCommand(createCmd)

	createCmd.Flags().Bool("json", false, "Output the configuration in JSON")
	createCmd.Flags().Bool("yaml", false, "Output the configuration in YAML")
	createCmd.MarkFlagsMutuallyExclusive("json", "yaml")

	createCmd.Flags().Bool("no-pretty", false, "Disable pretty printing JSON")
	createCmd.MarkFlagsMutuallyExclusive("no-pretty", "yaml")

	createCmd.Flags().StringP("output", "o", "", "Write the configuration to a file. This will overwrite anything already in the file")
}
