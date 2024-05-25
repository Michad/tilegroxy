package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Pre-populate (seed) the cache",
	Long: `Pre-populates the cache for a given layer for a given area (bounding box) for a range of zoom levels. 
	
	Be mindful that the higher the zoom level (the more you "zoom in"), exponentially more tiles will need to be seeded for a given area. For instance, while zoom level 1 only requires 4 tiles to cover the planet, zoom level 10 requires over a million tiles.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("seed called")
	},
}

func init() {
	rootCmd.AddCommand(seedCmd)

	seedCmd.Flags().StringP("layer", "l", "", "The ID of the layer to seed")
	seedCmd.MarkFlagRequired("layer")
	seedCmd.Flags().UintSliceP("zoom", "z", []uint{0, 1, 2, 3, 4, 5}, "The zoom level(s) to seed")
	seedCmd.Flags().Float32P("min-latitude", "s", -90, "The minimum latitude to seed. The south side of the bounding box")
	seedCmd.Flags().Float32P("max-latitude", "n", 90, "The maximum latitude to seed. The north side of the bounding box")
	seedCmd.Flags().Float32P("min-longitude", "w", -180, "The minimum longitude to seed. The west side of the bounding box")
	seedCmd.Flags().Float32P("max-longitude", "e", 180, "The maximum longitude to seed. The east side of the bounding box")
	seedCmd.Flags().Bool("force", false, "Perform the seeding even if it'll produce an excessive number of tiles. Normally seeds over 10k tiles will error out")
	seedCmd.Flags().Uint16P("threads", "t", 1, "How many concurrent requests to use to perform seeding. Be mindful of spamming upstream providers")
	// seedCmd.Flags().String("cache", "all", "Which cache to populate. Requires an ID")
}
