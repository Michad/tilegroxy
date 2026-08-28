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
	"fmt"

	tg "github.com/Michad/tilegroxy/pkg/entry"
	"github.com/spf13/cobra"
)

var cachePurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Deletes all cache entries for a layer",
	Long: `Deletes every cache entry belonging to a single layer.

This only works for cache backends that support it. Currently that's just the memory cache; other
backends report that they don't support this rather than erroring, since most cache backends can't
enumerate or bulk-delete their own keys.

Example:

	tilegroxy cache purge -c test_config.yml -l osm`,
	Run: runCachePurge,
}

func runCachePurge(cmd *cobra.Command, _ []string) {
	layerName, err := cmd.Flags().GetString("layer")
	out := rootCmd.OutOrStdout()

	if err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	cfg, err := extractConfigFromCommand(cmd, nil)
	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
		return
	}

	err = tg.PurgeCache(cfg, tg.PurgeOptions{LayerName: layerName}, out)

	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err.Error())
		exit(1)
	}
}

func init() {
	initCachePurge()
}

func initCachePurge() {
	cacheCmd.AddCommand(cachePurgeCmd)

	cachePurgeCmd.Flags().StringP("layer", "l", "", "The ID of the layer to purge from the cache")
	err := cachePurgeCmd.MarkFlagRequired("layer")

	if err != nil {
		panic(err)
	}
}
