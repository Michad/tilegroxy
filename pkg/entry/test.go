package tg

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"text/tabwriter"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type TestOptions struct {
	LayerNames []string
	Z          int
	X          int
	Y          int
	NumThread  uint16
	NoCache    bool
}

func Test(cfg *config.Config, opts TestOptions, out io.Writer) (uint32, error) {
	ctx := pkg.BackgroundContext()

	layerObjects, _, err := ConfigToEntities(*cfg)

	if err != nil {
		return 0, err
	}

	if len(opts.LayerNames) == 0 {
		opts.LayerNames = layerObjects.ListLayerIds()
	}

	//Generate the full list of requests to process
	tileRequests := make([]pkg.TileRequest, 0)

	for _, layerName := range opts.LayerNames {
		req := pkg.TileRequest{LayerName: layerName, Z: opts.Z, X: opts.X, Y: opts.Y}
		_, err := req.GetBounds()

		if err != nil {
			return 0, err
		}

		layer := layerObjects.FindLayer(ctx, layerName)

		if layer == nil {
			return 0, fmt.Errorf("invalid layer name: %v", layerName)
		}

		tileRequests = append(tileRequests, req)
	}

	numReq := len(tileRequests)

	if opts.NumThread > uint16(numReq) {
		fmt.Fprintln(os.Stderr, "Warning: more threads requested than tiles")
		opts.NumThread = uint16(numReq)
	}

	//Split up all the requests for N threads
	numReqPerThread := int(math.Floor(float64(numReq) / float64(opts.NumThread)))
	var reqSplit [][]pkg.TileRequest

	for i := 0; i < int(opts.NumThread); i++ {
		chunkStart := i * numReqPerThread
		var chunkEnd uint
		if i == int(opts.NumThread)-1 {
			chunkEnd = uint(numReq)
		} else {
			chunkEnd = uint(math.Min(float64(chunkStart+numReqPerThread), float64(numReq)))
		}

		reqSplit = append(reqSplit, tileRequests[chunkStart:chunkEnd])
	}

	//Start processing all the tile requests over N threads
	var wg sync.WaitGroup
	errCount := uint32(0)

	writer := tabwriter.NewWriter(out, 1, 4, 4, ' ', tabwriter.StripEscape)
	fmt.Fprintln(writer, "Thread\tLayer\tGenerated\tCache Write\tCache Read\tError\t")

	for t := int(0); t < len(reqSplit); t++ {
		wg.Add(1)
		go func(t int, myReqs []pkg.TileRequest) {
			ctx2 := pkg.BackgroundContext()

			for _, req := range myReqs {
				layer := layerObjects.FindLayer(ctx2, req.LayerName)
				img, layerErr := layer.RenderTileNoCache(ctx2, req)
				var cacheWriteError error
				var cacheReadError error

				if !opts.NoCache && layerErr == nil {
					cacheWriteError = layer.Cache.Save(req, img)
					if cacheWriteError == nil {
						var img2 *pkg.Image
						img2, cacheReadError = layer.Cache.Lookup(req)
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
					if opts.NoCache {
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
	return errCount, nil
}
