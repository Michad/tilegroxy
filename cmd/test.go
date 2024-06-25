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
	"math"
	"os"
	"slices"
	"strconv"
	"sync"
	"text/tabwriter"

	"sync/atomic"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/spf13/cobra"
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
		out := rootCmd.OutOrStdout()

		if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
			fmt.Fprintf(out, "Error: %v", err)
			exit(1)
		}

		_, layerObjects, _, err := parseConfigIntoStructs(cmd)

		if err != nil {
			fmt.Fprintf(out, "Error: %v", err)
			exit(1)
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
		tileRequests := make([]internal.TileRequest, 0)

		for _, layerName := range layerNames {
			req := internal.TileRequest{LayerName: layerName, Z: int(z), X: int(x), Y: int(y)}
			_, err := req.GetBounds()

			if err != nil {
				fmt.Fprintf(out, "Error: %v", err)
				exit(1)
			}

			layer := layerMap[layerName]

			if layer == nil {
				fmt.Fprintf(out, "Error: Invalid layer name: %v", layer)
				exit(1)
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
		var reqSplit [][]internal.TileRequest

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
		errCount := uint32(0)

		writer := tabwriter.NewWriter(os.Stdout, 1, 4, 4, ' ', tabwriter.StripEscape)
		fmt.Fprintln(writer, "Thread\tLayer\tGenerated\tCache Write\tCache Read\tError\t")

		for t := int(0); t < len(reqSplit); t++ {
			wg.Add(1)
			go func(t int, myReqs []internal.TileRequest) {

				for _, req := range myReqs {
					layer := layerMap[req.LayerName]
					img, layerErr := layer.RenderTileNoCache(internal.BackgroundContext(), req)
					var cacheWriteError error
					var cacheReadError error

					if !noCache && layerErr == nil {
						cacheWriteError = (*layer.Cache).Save(req, img)
						if cacheWriteError == nil {
							var img2 *internal.Image
							img2, cacheReadError = (*layer.Cache).Lookup(req)
							if cacheReadError == nil {
								if img2 == nil {
									cacheReadError = errors.New("no result from cache lookup")
								} else if !slices.Equal(*img, *img2) {
									cacheReadError = errors.New("cache result doesn't match what we put into cache")
								}
							}
						}
					}

					if layerErr != nil || cacheWriteError != nil || cacheReadError != nil {
						atomic.AddUint32(&errCount, 1)
					}

					//Output the result into the table
					resultStr := strconv.Itoa(t) + "\t" + req.LayerName + "\t"
					if layerErr != nil {
						resultStr += "No\tN/A\tN/A\t\xff" + layerErr.Error() + "\xff\t"
					} else {
						if noCache {
							resultStr += "Yes\tN/A\tN/A\tNone\t"
						} else if cacheWriteError != nil {
							resultStr += "Yes\tNo\tN/A\t\xff" + cacheWriteError.Error() + "\xff\t"
						} else if cacheReadError != nil {
							resultStr += "Yes\tYes\tNo\t\xff" + cacheReadError.Error() + "\xff\t"
						} else {
							resultStr += "Yes\tYes\tYes\tNone\t"
						}
					}
					fmt.Fprintln(writer, resultStr)

				}

				wg.Done()
			}(t, reqSplit[t])
		}

		wg.Wait()

		writer.Flush()

		fmt.Fprintf(out, "Completed with %v failures\n", errCount)

		if errCount > 0 {
			if errCount > 125 {
				exit(125)
			}
			exit(int(errCount))
		}
	},
}

func init() {
	InitTest()
}

func InitTest() {
	rootCmd.AddCommand(testCmd)

	testCmd.Flags().StringSliceP("layer", "l", []string{}, "The ID(s) of the layer to test. Tests all layers by default")
	testCmd.Flags().UintP("z-coordinate", "z", 10, "The z coordinate to use to test")
	testCmd.Flags().UintP("x-coordinate", "x", 123, "The x coordinate to use to test")
	testCmd.Flags().UintP("y-coordinate", "y", 534, "The y coordinate to use to test")
	testCmd.Flags().Bool("no-cache", false, "Don't write to the cache. The Cache configuration must still be syntactically valid")
	testCmd.Flags().Uint16P("threads", "t", 1, "How many layers to test at once. Be mindful of spamming upstream providers")
	//TODO: output in custom format or write to file
}
