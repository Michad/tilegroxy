// Copyright 2026 Michael Davis
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
	"fmt"

	"github.com/Michad/tilegroxy/pkg/config"
	tg "github.com/Michad/tilegroxy/pkg/entry"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff FILE",
	Short: "Compares two configurations",
	Long: `Compares your current configuration against a new configuration, outputting what changed and whether serve --hot-reload can apply it without a restart.  Outputs everything that gets added, removed, or modified.  The output isn't a simple line-by-line diff, instead it's contextually aware of the tilegroxy config format, so a new layer gets recorded as one addition.  

The default output is a readable, colored text mode. Consider using json or yaml for scripted use cases. The presence of a top-level 'restart' key indicates whether a restart is needed for CICD purposes.

Exits with a status code of 0 if the configurations match and 1 if they differ. 

Example:
	tilegroxy config diff -c current.yml candidate.yml`,
	Args: cobra.ExactArgs(1),
	Run:  runDiff,
}

func runDiff(cmd *cobra.Command, args []string) {
	asJSON, err1 := cmd.Flags().GetBool("json")
	asYAML, err2 := cmd.Flags().GetBool("yaml")
	asTable, err3 := cmd.Flags().GetBool("table")
	asMarkdown, err4 := cmd.Flags().GetBool("markdown")
	noColor, err5 := cmd.Flags().GetBool("no-color")
	out := cmd.OutOrStdout()

	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
		return
	}

	format := tg.DiffFormatText

	switch {
	case asJSON:
		format = tg.DiffFormatJSON
	case asYAML:
		format = tg.DiffFormatYAML
	case asTable:
		format = tg.DiffFormatTable
	case asMarkdown:
		format = tg.DiffFormatMarkdown
	}

	oldCfg, err := extractConfigFromCommand(cmd, nil)
	if err != nil {
		fmt.Fprintf(out, "Invalid configuration: %v\n", err.Error())
		exit(1)
		return
	}

	newCfg, err := config.LoadConfigFromFile(args[0])
	if err != nil {
		fmt.Fprintf(out, "Invalid configuration %v: %v\n", args[0], err.Error())
		exit(1)
		return
	}

	different, err := tg.DiffConfig(oldCfg, &newCfg, tg.DiffOptions{Format: format, Color: !noColor}, out)
	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
		return
	}

	if different {
		exit(1)
		return
	}
}

func init() {
	initDiff()
}

func initDiff() {
	configCmd.AddCommand(diffCmd)

	diffCmd.Flags().Bool("json", false, "Output the differences in JSON")
	diffCmd.Flags().Bool("yaml", false, "Output the differences in YAML")
	diffCmd.Flags().Bool("table", false, "Output the differences as a table with one row per change")
	diffCmd.Flags().Bool("markdown", false, "Output the differences as a markdown table with one row per change")
	diffCmd.Flags().Bool("no-color", false, "Turn off the colorization of the default output")
	diffCmd.MarkFlagsMutuallyExclusive("json", "yaml", "table", "markdown", "no-color")
}
