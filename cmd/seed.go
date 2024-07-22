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

	"github.com/Michad/tilegroxy/pkg"
	tg "github.com/Michad/tilegroxy/pkg/entry"
	"github.com/spf13/cobra"
)

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Pre-populate (seed) the cache",
	Long: `Pre-populates the cache for a given layer for a given area (bounding box) for a range of zoom levels. 
	
Be mindful that the greater the zoom level (the more you "zoom in"), exponentially more tiles will need to be seeded for a given area. For instance, while zoom level 1 only requires 4 tiles to cover the planet, zoom level 10 requires over a million tiles.

Example:

	tilegroxy seed -c test_config.yml -l osm -z 2 -v -t 7 -z 0 -z 1 -z 3 -z 4`,
	Run: runSeed,
}

func runSeed(cmd *cobra.Command, args []string) {
	layerName, err1 := cmd.Flags().GetString("layer")
	zoom, err2 := cmd.Flags().GetUintSlice("zoom")
	minLat, err3 := cmd.Flags().GetFloat32("min-latitude")
	maxLat, err4 := cmd.Flags().GetFloat32("max-latitude")
	minLon, err5 := cmd.Flags().GetFloat32("min-longitude")
	maxLon, err6 := cmd.Flags().GetFloat32("max-longitude")
	force, err7 := cmd.Flags().GetBool("force")
	numThread, err8 := cmd.Flags().GetUint16("threads")
	verbose, err9 := cmd.Flags().GetBool("verbose")
	out := rootCmd.OutOrStdout()

	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9); err != nil {
		fmt.Fprintf(out, "Error: %v", err)
		exit(1)
		return
	}

	cfg, err := extractConfigFromCommand(cmd)
	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
		return
	}

	b := pkg.Bounds{South: float64(minLat), West: float64(minLon), North: float64(maxLat), East: float64(maxLon)}

	err = tg.Seed(cfg,
		tg.SeedOptions{
			Zoom:      zoom,
			Bounds:    b,
			LayerName: layerName,
			Force:     force,
			Verbose:   verbose,
			NumThread: numThread},
		out)

	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err.Error())
		exit(1)
	}
}

func init() {
	initSeed()
}

func initSeed() {
	rootCmd.AddCommand(seedCmd)

	seedCmd.Flags().StringP("layer", "l", "", "The ID of the layer to seed")
	seedCmd.MarkFlagRequired("layer")
	seedCmd.Flags().BoolP("verbose", "v", false, "Output verbose information including every tile being requested and success or error status")
	seedCmd.Flags().UintSliceP("zoom", "z", []uint{0, 1, 2, 3, 4, 5}, "The zoom level(s) to seed")
	seedCmd.Flags().Float32P("min-latitude", "s", -90, "The minimum latitude to seed. The south side of the bounding box")
	seedCmd.Flags().Float32P("max-latitude", "n", 90, "The maximum latitude to seed. The north side of the bounding box")
	seedCmd.Flags().Float32P("min-longitude", "w", -180, "The minimum longitude to seed. The west side of the bounding box")
	seedCmd.Flags().Float32P("max-longitude", "e", 180, "The maximum longitude to seed. The east side of the bounding box")
	seedCmd.Flags().Bool("force", false, "Perform the seeding even if it'll produce an excessive number of tiles. Without this flag seeds over 10k tiles will error out. \nWarning: Overriding this protection absolutely can cause an Out-of-Memory error")
	seedCmd.Flags().Uint16P("threads", "t", 1, "How many concurrent requests to use to perform seeding. Be mindful of spamming upstream providers")
	// TODO: support some way to support writing just to a specific cache when Multi cache is being used
}
