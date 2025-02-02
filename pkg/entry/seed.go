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
	"slices"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

const maxCount = 10000

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
		newTileRequests, err := createTileRequests(z, len(tileRequests), opts)
		if err != nil {
			return err
		}
		tileRequests = slices.Concat(tileRequests, *newTileRequests)
	}

	if opts.Verbose {
		fmt.Fprintf(out, "Number of tile requests: %v\n", len(tileRequests))
	}

	numReq := len(tileRequests)

	if numReq > math.MaxUint16 {
		return fmt.Errorf("more than %v tiles requested", math.MaxUint16)
	}

	if opts.NumThread > uint16(numReq) {
		fmt.Fprintln(os.Stderr, "Warning: more threads requested than tiles")
		opts.NumThread = uint16(numReq)
	}

	chunkSize := int(math.Floor(float64(numReq) / float64(opts.NumThread)))

	reqSplit := make([][]pkg.TileRequest, 0, int(opts.NumThread))

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

	for t := range reqSplit {
		wg.Add(1)
		go seedThread(&wg, opts, out, layerGroup, t, reqSplit[t])
	}

	wg.Wait()
	if opts.Verbose {
		fmt.Fprintf(out, "Completed seeding")
	}
	return nil
}

func createTileRequests(z uint, curCount int, opts SeedOptions) (*[]pkg.TileRequest, error) {
	if z > pkg.MaxZoom {
		return nil, fmt.Errorf("zoom must be less than %v", pkg.MaxZoom)
	}
	tileRequests, err := opts.Bounds.FindTiles(opts.LayerName, z, opts.Force)

	if err != nil || (curCount > maxCount && !opts.Force) {
		count := uint64(curCount) // #nosec G115

		if err != nil {
			var tilesError pkg.TooManyTilesError

			if errors.As(err, &tilesError) {
				count = tilesError.NumTiles
			} else {
				return nil, err
			}
		}

		return nil, fmt.Errorf("too many tiles to seed (%v > %v). %v",
			count,
			pkg.Ternary(count > math.MaxInt32, math.MaxInt32, maxCount),
			pkg.Ternary(count > math.MaxInt32, "", "Run with --force if you're sure you want to generate this many tiles"))
	}
	return tileRequests, nil
}

func seedThread(wg *sync.WaitGroup, opts SeedOptions, out io.Writer, layerGroup *layer.LayerGroup, t int, myReqs []pkg.TileRequest) {
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
}
