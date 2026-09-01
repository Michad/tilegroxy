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
	"fmt"

	tg "github.com/Michad/tilegroxy/pkg/entry"
	"github.com/spf13/cobra"
)

const maxExitCode = 125

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test layers and cache work",
	Long: `Tests that everything is working end-to-end for all or some layers including caching. This goes further than 'config check' and instead of just validating the configuration can be parsed it actually makes sample request(s) and populates the result in the cache. This is similar to running 'seed' for a single tile or standing up the server and making a cURL request for each layer. The output will list each layer and the status, with any error encountered if applicable.

If you don't specify a tile coordinate with -z/-x/-y, one is picked automatically for each layer from its configured bounds and minzoom/maxzoom. A layer with neither bounds nor a zoom range configured falls back to a fixed default tile that might be outside the area your layer actually serves

A layer using a pattern is tested through its "examples" configurations if available. A pattern layer with no examples configured is skipped with a warning.

Use --file to write a summary to a file. The standard output will still render in stdout, use a bash redirect if you want that in a file. If using both --file and --json, only the file output will be in JSON

Example:

	tilegroxy test -c test_config.yml -l osm -z 10 -x 123 -y 534`,
	Run: runTest,
}

func runTest(cmd *cobra.Command, _ []string) {
	layerNames, err1 := cmd.Flags().GetStringSlice("layer")
	z, err2 := cmd.Flags().GetInt("z-coordinate")
	x, err3 := cmd.Flags().GetInt("y-coordinate")
	y, err4 := cmd.Flags().GetInt("x-coordinate")
	noCache, err5 := cmd.Flags().GetBool("no-cache")
	numThread, err6 := cmd.Flags().GetUint16("threads")
	jsonOut, err7 := cmd.Flags().GetBool("json")
	filePath, err8 := cmd.Flags().GetString("file")
	out := rootCmd.OutOrStdout()

	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8); err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	coordinatesSet := cmd.Flags().Changed("z-coordinate") || cmd.Flags().Changed("x-coordinate") || cmd.Flags().Changed("y-coordinate")

	cfg, err := extractConfigFromCommand(cmd, nil)
	if err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	errCount, err := tg.Test(cfg, tg.TestOptions{
		LayerNames:     layerNames,
		Z:              z,
		X:              x,
		Y:              y,
		CoordinatesSet: coordinatesSet,
		NumThread:      numThread,
		NoCache:        noCache,
		JSON:           jsonOut,
		FilePath:       filePath,
	}, out)

	if err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	if !jsonOut {
		fmt.Fprintf(out, "Completed with %v failures\n", errCount)
	}

	if errCount > 0 {
		if errCount > maxExitCode {
			exit(maxExitCode)
			return
		}
		exit(int(errCount))
		return
	}
}

func init() {
	initTest()
}

func initTest() {
	rootCmd.AddCommand(testCmd)

	testCmd.Flags().StringSliceP("layer", "l", []string{}, "The ID(s) of the layer to test. Tests all layers by default")
	testCmd.Flags().IntP("z-coordinate", "z", 10, "The z coordinate to use to test.")  //nolint:mnd
	testCmd.Flags().IntP("x-coordinate", "x", 123, "The x coordinate to use to test.") //nolint:mnd
	testCmd.Flags().IntP("y-coordinate", "y", 534, "The y coordinate to use to test.") //nolint:mnd
	testCmd.Flags().Bool("no-cache", false, "Don't write to the cache. The Cache configuration must still be syntactically valid")
	testCmd.Flags().Uint16P("threads", "t", 1, "How many layers to test at once. Be mindful of spamming upstream providers")
	testCmd.Flags().Bool("json", false, "Output in JSON. If used with --file this only outputs the written file, not standard out")
	testCmd.Flags().StringP("file", "f", "", "Write a run summary to this file.The file is JSON if --json is set, otherwise plain text")
}
