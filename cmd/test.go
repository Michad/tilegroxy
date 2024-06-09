package cmd

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"text/tabwriter"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/spf13/cobra"
	"go.uber.org/atomic"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test layers and cache work",
	Long: `Tests that everything is working end-to-end for all or some layers including caching. This goes further than 'config check' and instead of just validating the configuration can be parsed it actually makes sample request(s) and populates the result in the cache. This is similar to running 'seed' for a single tile or standing up the server and making a cURL request for each layer. The output will list each layer and the status, with any error encountered if applicable.

This test uses an arbitrary tile coordinate to test with. The default coordinate might be outside the bounds of your map layer, there is currently no logic to consider the bounds configured for each layer; you will need to specify an applicable tile to use.  It is not recommended to use 0,0,0 due to potential performance issues when dealing with large data. If your cache is configured to prevent overwriting existing items you might need to pick a distinct tile each time you run the test or run with cache disabled (--no-cache).

Example:

	tilegroxy test -c test_config.yml -l osm -z 10 -x 123 -y 534`,
	Run: func(cmd *cobra.Command, args []string) {
		layerNames, err1 := cmd.Flags().GetStringSlice("layer")
		z, err2 := cmd.Flags().GetUint("z-coordinate")
		x, err3 := cmd.Flags().GetUint("y-coordinate")
		y, err4 := cmd.Flags().GetUint("x-coordinate")
		noCache, err5 := cmd.Flags().GetBool("no-cache")
		numThread, err6 := cmd.Flags().GetUint16("threads")

		if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
			fmt.Printf("Error: %v", err)
			os.Exit(1)
		}

		_, layerObjects, _, err := parseConfigIntoStructs(cmd)

		if err != nil {
			fmt.Printf("Error: %v", err)
			os.Exit(1)
		}
		layerMap := make(map[string]*layers.Layer)

		for _, l := range layerObjects {
			layerMap[l.Id] = l
		}

		if len(layerNames) == 0 {
			for _, l := range layerObjects {
				layerNames = append(layerNames, l.Id)
			}
		}

		//Generate the full list of requests to process
		tileRequests := make([]pkg.TileRequest, 0)

		for _, layerName := range layerNames {
			req := pkg.TileRequest{LayerName: layerName, Z: int(z), X: int(x), Y: int(y)}
			_, err := req.GetBounds()

			if err != nil {
				fmt.Printf("Error: %v", err)
				os.Exit(1)
			}

			layer := layerMap[layerName]

			if layer == nil {
				fmt.Printf("Error: Invalid layer name: %v", layer)
				os.Exit(1)
			}

			tileRequests = append(tileRequests, req)
		}

		numReq := len(tileRequests)

		if numThread > uint16(numReq) {
			fmt.Fprintln(os.Stderr, "Warning: more threads requested than tiles")
			numThread = uint16(numReq)
		}

		//Split up all the requests for N threads
		numReqPerThread := int(math.Floor(float64(numReq) / float64(numThread)))
		var reqSplit [][]pkg.TileRequest

		for i := 0; i < int(numThread); i++ {
			chunkStart := i * numReqPerThread
			var chunkEnd uint
			if i == int(numThread)-1 {
				chunkEnd = uint(numReq)
			} else {
				chunkEnd = uint(math.Min(float64(chunkStart+numReqPerThread), float64(numReq)))
			}

			reqSplit = append(reqSplit, tileRequests[chunkStart:chunkEnd])
		}

		//Start processing all the tile requests over N threads
		var wg sync.WaitGroup
		var errCounter = atomic.NewUint32(0)

		writer := tabwriter.NewWriter(os.Stdout, 1, 4, 4, ' ', tabwriter.StripEscape)
		fmt.Fprintln(writer, "Thread\tLayer\tGenerated\tCached\tError\t")

		for t := int(0); t < len(reqSplit); t++ {
			wg.Add(1)
			go func(t int, myReqs []pkg.TileRequest) {

				for _, req := range myReqs {
					layer := layerMap[req.LayerName]
					img, layerErr := layer.RenderTileNoCache(req)
					var cacheError error

					if !noCache && layerErr == nil {
						cacheError = (*layer.Cache).Save(req, img)
					}

					if layerErr != nil || cacheError != nil {
						errCounter.Inc()
					}

					//Output the result into the table
					resultStr := strconv.Itoa(t) + "\t" + req.LayerName + "\t"
					if layerErr != nil {
						resultStr += "No\tN/A\t\xff" + layerErr.Error() + "\xff\t"
					} else {
						if cacheError != nil {
							resultStr += "Yes\tNo\t\xff" + cacheError.Error() + "\xff\t"
						} else {
							resultStr += "Yes\tYes\tNone\t"
						}
					}
					fmt.Fprintln(writer, resultStr)

				}

				wg.Done()
			}(t, reqSplit[t])
		}

		wg.Wait()

		writer.Flush()

		errCount := errCounter.Load()
		fmt.Printf("Completed with %v failures\n", errCount)

		if errCount > 0 {
			if errCount > 125 {
				os.Exit(125)
			}
			os.Exit(int(errCount))
		}
	},
}

func init() {
	rootCmd.AddCommand(testCmd)

	testCmd.Flags().StringSliceP("layer", "l", []string{}, "The ID(s) of the layer to test. Tests all layers by default")
	testCmd.Flags().UintP("z-coordinate", "z", 10, "The z coordinate to use to test")
	testCmd.Flags().UintP("x-coordinate", "x", 123, "The x coordinate to use to test")
	testCmd.Flags().UintP("y-coordinate", "y", 534, "The y coordinate to use to test")
	testCmd.Flags().Bool("no-cache", false, "Only validate the layer and not the cache")
	testCmd.Flags().Uint16P("threads", "t", 1, "How many layers to test at once. Be mindful of spamming upstream providers")
	//TODO: output in custom format
}
