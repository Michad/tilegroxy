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

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test layers and cache work",
	Long: `Tests that everything is working end-to-end for all or some layers including caching. This goes further than 'config check' and instead of just validating the configuration can be parsed it actually makes sample request(s) and populates the result in the cache. This is similar to running 'seed' for a single tile or standing up the server and making a cURL request for each layer. The output will list each layer and the status, with any error encountered if applicable.

This test uses an arbitrary tile coordinate to test with. The default coordinate might be outside the bounds of your map layer, there is currently no logic to consider the bounds configured for each layer; you will need to specify an applicable tile to use.  It is not recommended to use 0,0,0 due to potential performance issues when dealing with large data. If your cache is configured to prevent overwriting existing items you might need to pick a distinct tile each time you run the test or run with cache disabled (--no-cache).

Example:

	tilegroxy test -c test_config.yml -l osm -z 10 -x 123 -y 534`,
	Run: runTest,
}

func runTest(cmd *cobra.Command, args []string) {
	layerNames, err1 := cmd.Flags().GetStringSlice("layer")
	z, err2 := cmd.Flags().GetUint("z-coordinate")
	x, err3 := cmd.Flags().GetUint("y-coordinate")
	y, err4 := cmd.Flags().GetUint("x-coordinate")
	noCache, err5 := cmd.Flags().GetBool("no-cache")
	numThread, err6 := cmd.Flags().GetUint16("threads")
	out := rootCmd.OutOrStdout()

	if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	cfg, err := extractConfigFromCommand(cmd)
	if err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	errCount, err := tg.Test(cfg, tg.TestOptions{LayerNames: layerNames, Z: int(z), X: int(x), Y: int(y), NumThread: numThread, NoCache: noCache}, out)

	if err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	fmt.Fprintf(out, "Completed with %v failures\n", errCount)

	if errCount > 0 {
		if errCount > 125 {
			exit(125)
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
	testCmd.Flags().UintP("z-coordinate", "z", 10, "The z coordinate to use to test")
	testCmd.Flags().UintP("x-coordinate", "x", 123, "The x coordinate to use to test")
	testCmd.Flags().UintP("y-coordinate", "y", 534, "The y coordinate to use to test")
	testCmd.Flags().Bool("no-cache", false, "Don't write to the cache. The Cache configuration must still be syntactically valid")
	testCmd.Flags().Uint16P("threads", "t", 1, "How many layers to test at once. Be mindful of spamming upstream providers")
	//TODO: output in custom format or write to file
}
