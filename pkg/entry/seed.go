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

package tg

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type SeedOptions struct {
	Zoom      []uint
	Bounds    pkg.Bounds
	LayerName string
	Force     bool
	Verbose   bool
	NumThread uint16
}

func Seed(cfg *config.Config, opts SeedOptions, out io.Writer) error {
	ctx := pkg.BackgroundContext()

	if opts.NumThread == 0 {
		return errors.New("threads must be above 0")
	}

	layerGroup, _, err := configToEntities(*cfg)

	if err != nil {
		return err
	}

	layer := layerGroup.FindLayer(ctx, opts.LayerName)

	if layer == nil {
		return errors.New("invalid layer")
	}

	tileRequests := make([]pkg.TileRequest, 0)

	for _, z := range opts.Zoom {
		if z > pkg.MaxZoom {
			return fmt.Errorf("zoom must be less than %v", pkg.MaxZoom)
		}
		newTiles, err := opts.Bounds.FindTiles(opts.LayerName, uint(z), opts.Force)

		if newTiles != nil {
			tileRequests = append(tileRequests, (*newTiles)...)
		}

		if err != nil || (len(tileRequests) > 10000 && !opts.Force) {
			count := len(tileRequests)

			if err != nil {
				var tilesError pkg.TooManyTilesError

				if errors.As(err, &tilesError) {
					count = int(tilesError.NumTiles)
				} else {
					return err
				}
			}

			return fmt.Errorf("too many tiles to seed (%v > %v). %v",
				count,
				pkg.Ternary(count > math.MaxInt32, math.MaxInt32, 10000),
				pkg.Ternary(count > math.MaxInt32, "", "Run with --force if you're sure you want to generate this many tiles"))
		}
	}

	if opts.Verbose {
		fmt.Fprintf(out, "Number of tile requests: %v\n", len(tileRequests))
	}

	numReq := len(tileRequests)

	if opts.NumThread > uint16(numReq) {
		fmt.Fprintln(os.Stderr, "Warning: more threads requested than tiles")
		opts.NumThread = uint16(numReq)
	}

	chunkSize := int(math.Floor(float64(numReq) / float64(opts.NumThread)))

	var reqSplit [][]pkg.TileRequest

	for i := range int(opts.NumThread) {
		chunkStart := i * chunkSize
		var chunkEnd uint
		if i == int(opts.NumThread)-1 {
			chunkEnd = uint(numReq)
		} else {
			chunkEnd = uint(math.Min(float64(chunkStart+chunkSize), float64(numReq)))
		}

		reqSplit = append(reqSplit, tileRequests[chunkStart:chunkEnd])
	}

	var wg sync.WaitGroup

	for t := range len(reqSplit) {
		wg.Add(1)
		go func(t int, myReqs []pkg.TileRequest) {
			if opts.Verbose {
				fmt.Fprintf(out, "Created thread %v with %v tiles\n", t, len(myReqs))
			}
			for _, req := range myReqs {
				_, tileErr := layerGroup.RenderTile(pkg.BackgroundContext(), req)

				if opts.Verbose {
					var status string
					if tileErr == nil {
						status = "OK"
					} else {
						status = tileErr.Error()
					}

					fmt.Fprintf(out, "Thread %v - %v = %v\n", t, req, status)
				}
			}
			if opts.Verbose {
				fmt.Fprintf(out, "Finished thread %v\n", t)
			}
			wg.Done()
		}(t, reqSplit[t])
	}

	wg.Wait()
	if opts.Verbose {
		fmt.Fprintf(out, "Completed seeding")
	}
	return nil
}
