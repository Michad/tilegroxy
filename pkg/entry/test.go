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
	"context"
	"encoding/json"
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
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type TestOptions struct {
	LayerNames     []string
	Z              int
	X              int
	Y              int
	CoordinatesSet bool
	NumThread      uint16
	NoCache        bool
	JSON           bool
	FilePath       string
}

// the default tile used when a layer has no configured bounds/zoom to derive one from.
const (
	defaultZ = 10
	defaultX = 123
	defaultY = 534
)

func pickTile(l *layer.Layer, layerName string) pkg.TileRequest {
	hasBounds := l.Config.Bounds != (config.BoundsConfig{})
	hasZoom := l.Config.MinZoom != nil || l.Config.MaxZoom != nil

	if !hasBounds && !hasZoom {
		return pkg.TileRequest{LayerName: layerName, Z: defaultZ, X: defaultX, Y: defaultY}
	}

	minZoom := 0
	if l.Config.MinZoom != nil {
		minZoom = *l.Config.MinZoom
	}
	maxZoom := pkg.MaxZoom
	if l.Config.MaxZoom != nil {
		maxZoom = *l.Config.MaxZoom
	}

	z := uint((minZoom + maxZoom) / 2) //nolint:gosec // min/maxZoom are bounded well within int range

	bounds := pkg.WorldBounds()
	if hasBounds {
		bounds = pkg.Bounds{
			South: l.Config.Bounds.South,
			North: l.Config.Bounds.North,
			West:  l.Config.Bounds.West,
			East:  l.Config.Bounds.East,
		}
	}

	zoomRange, err := bounds.ConstructSingleZoomRange(z)
	if err != nil {
		return pkg.TileRequest{LayerName: layerName, Z: defaultZ, X: defaultX, Y: defaultY}
	}

	x := (zoomRange.XMin + zoomRange.XMax - 1) / 2 //nolint:mnd // Midpoint of an exclusive-max range
	y := (zoomRange.YMin + zoomRange.YMax - 1) / 2 //nolint:mnd // Midpoint of an exclusive-max range

	return pkg.TileRequest{LayerName: layerName, Z: int(z), X: x, Y: y}
}

// the set of layer names tested when none are given. A pattern layer's ID
// isn't a name, so it contributes its examples instead
func defaultLayerNames(layerObjects *layer.LayerGroup) []string {
	names := make([]string, 0, len(layerObjects.Layers()))

	for _, l := range layerObjects.Layers() {
		if !l.IsPattern() {
			names = append(names, l.ID)
			continue
		}

		if len(l.Config.Examples) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: skipping layer %v, a pattern layer needs examples to be tested\n", l.ID)
			continue
		}

		names = append(names, l.Config.Examples...)
	}

	return names
}

type TestSummary struct {
	Failures []TestFailure `json:"failures"`
	Tested   int           `json:"tested"`
	Failed   int           `json:"failed"`
}

type TestFailure struct {
	LayerName string `json:"layer"`
	Error     string `json:"error"`
}

func buildTileRequests(ctx context.Context, layerObjects *layer.LayerGroup, opts TestOptions) ([]pkg.TileRequest, error) {
	tileRequests := make([]pkg.TileRequest, 0, len(opts.LayerNames))

	for _, layerName := range opts.LayerNames {
		l := layerObjects.FindLayer(ctx, layerName)

		if l == nil {
			return nil, fmt.Errorf("invalid layer name: %v", layerName)
		}

		var req pkg.TileRequest
		if opts.CoordinatesSet {
			req = pkg.TileRequest{LayerName: layerName, Z: opts.Z, X: opts.X, Y: opts.Y}
		} else {
			req = pickTile(l, layerName)
		}

		if _, err := req.GetBounds(); err != nil {
			return nil, err
		}

		tileRequests = append(tileRequests, req)
	}

	return tileRequests, nil
}

func splitForThreads(tileRequests []pkg.TileRequest, numThread uint16) [][]pkg.TileRequest {
	numReq := len(tileRequests)
	numReqPerThread := int(math.Floor(float64(numReq) / float64(numThread)))
	reqSplit := make([][]pkg.TileRequest, 0, int(numThread))

	for i := range int(numThread) {
		chunkStart := i * numReqPerThread
		var chunkEnd uint
		if i == int(numThread)-1 {
			chunkEnd = uint(numReq)
		} else {
			chunkEnd = uint(math.Min(float64(chunkStart+numReqPerThread), float64(numReq)))
		}

		reqSplit = append(reqSplit, tileRequests[chunkStart:chunkEnd])
	}

	return reqSplit
}

func Test(cfg *config.Config, opts TestOptions, out io.Writer) (uint32, error) {
	ctx := pkg.BackgroundContext()

	ent, err := configToEntities(*cfg)

	if err != nil {
		return 0, err
	}

	defer ent.Close(ctx) //nolint:errcheck // Nothing actionable while tearing down a test run

	layerObjects := ent.LayerGroup

	if len(opts.LayerNames) == 0 {
		opts.LayerNames = defaultLayerNames(layerObjects)
	}

	tileRequests, err := buildTileRequests(ctx, layerObjects, opts)
	if err != nil {
		return 0, err
	}

	numReq := len(tileRequests)

	if numReq > math.MaxUint16 {
		return 0, fmt.Errorf("more than %v tiles requested", math.MaxUint16)
	}

	if opts.NumThread > uint16(numReq) {
		fmt.Fprintln(os.Stderr, "Warning: more threads requested than tiles")
		opts.NumThread = uint16(numReq)
	}

	reqSplit := splitForThreads(tileRequests, opts.NumThread)

	// The live per-tile text table is written to stdout unless we're in JSON mode with no file to
	// separately stream to - in that case stdout is reserved for the JSON summary alone.
	tableOut := out
	if opts.JSON && opts.FilePath == "" {
		tableOut = io.Discard
	}

	// Start processing all the tile requests over N threads
	var wg sync.WaitGroup
	errCount := uint32(0)
	var failuresMu sync.Mutex
	failures := make([]TestFailure, 0)

	writer := tabwriter.NewWriter(tableOut, 1, 4, 4, ' ', tabwriter.StripEscape) //nolint:mnd
	fmt.Fprintln(writer, "Thread\tLayer\tGenerated\tCache Write\tCache Read\tError\t")

	for t := range reqSplit {
		wg.Add(1)
		go testTileRequests(layerObjects, opts, &errCount, &failuresMu, &failures, writer, &wg, t, reqSplit[t])
	}

	wg.Wait()

	if err := writer.Flush(); err != nil {
		return errCount, err
	}

	summary := TestSummary{Tested: numReq, Failed: int(errCount), Failures: failures}

	if err := writeSummary(out, opts, summary); err != nil {
		return errCount, err
	}

	return errCount, nil
}

// writeSummary emits the run summary to --file (if set) and, when in JSON mode with no --file, to
// out as well since that's the only output stdout gets in that case.
func writeSummary(out io.Writer, opts TestOptions, summary TestSummary) error {
	if opts.FilePath != "" {
		file, err := os.Create(opts.FilePath) //nolint:gosec // Operator-supplied path from the CLI, not user input
		if err != nil {
			return err
		}
		defer file.Close() //nolint:errcheck // Best effort close after the summary is written

		if err := writeSummaryTo(file, opts.JSON, summary); err != nil {
			return err
		}
	}

	if opts.JSON && opts.FilePath == "" {
		return writeSummaryTo(out, true, summary)
	}

	return nil
}

func writeSummaryTo(w io.Writer, asJSON bool, summary TestSummary) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")

		return enc.Encode(summary)
	}

	fmt.Fprintf(w, "Tested %v layers, %v failures\n", summary.Tested, summary.Failed)

	for _, f := range summary.Failures {
		fmt.Fprintf(w, "  %v: %v\n", f.LayerName, f.Error)
	}

	return nil
}

func testTileRequests(layerObjects *layer.LayerGroup, opts TestOptions, errCount *uint32, failuresMu *sync.Mutex, failures *[]TestFailure, writer *tabwriter.Writer, wg *sync.WaitGroup, t int, myReqs []pkg.TileRequest) {
	ctx := pkg.BackgroundContext()

	for _, req := range myReqs {
		layer := layerObjects.FindLayer(ctx, req.LayerName)
		img, layerErr := layer.RenderTileNoCache(ctx, req)
		var cacheWriteError error
		var cacheReadError error

		if !opts.NoCache && layerErr == nil {
			cacheWriteError = layer.Cache.Save(ctx, req, img)
			if cacheWriteError == nil {
				var img2 *pkg.Image
				img2, cacheReadError = layer.Cache.Lookup(ctx, req)
				if cacheReadError == nil {
					if img2 == nil {
						cacheReadError = errors.New("no result from cache lookup")
					} else if !slices.Equal(img.Content, img2.Content) {
						cacheReadError = errors.New("cache result doesn't match what we put into cache")
					}
				}
			}
		}

		layerFailure := layerErr
		if layerFailure == nil {
			layerFailure = cacheWriteError
		}
		if layerFailure == nil {
			layerFailure = cacheReadError
		}

		if layerFailure != nil {
			atomic.AddUint32(errCount, 1)

			failuresMu.Lock()
			*failures = append(*failures, TestFailure{LayerName: req.LayerName, Error: layerFailure.Error()})
			failuresMu.Unlock()
		}

		// Output the result into the table
		resultStr := strconv.Itoa(t) + "\t" + req.LayerName + "\t"
		if layerErr != nil {
			resultStr += "No\tN/A\tN/A\t\xff" + layerErr.Error() + "\xff\t"
		} else {
			if opts.NoCache { //nolint:gocritic
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
}
