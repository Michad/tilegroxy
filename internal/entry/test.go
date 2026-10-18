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

package entry

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

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/internal/seed"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
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
	UserID         string
	TenantID       string
}

// Used when a layer has no center, bounds or zoom to derive a tile from
const (
	defaultZ = 10
	defaultX = 123
	defaultY = 534
)

const (
	centerLatIndex  = 1
	centerZoomIndex = 2
)

func pickTile(l *layers.Layer, layerName string) pkg.TileRequest {
	md := l.Metadata().Advertised
	hasBounds := md.Bounds != (config.BoundsConfig{})
	hasZoom := md.MinZoom != nil || md.MaxZoom != nil
	hasCenter := len(md.Center) > centerLatIndex

	if !hasBounds && !hasZoom && !hasCenter {
		return pkg.TileRequest{LayerName: layerName, Z: defaultZ, X: defaultX, Y: defaultY}
	}

	minZoom, maxZoom := md.ZoomRange()
	z := uint((minZoom + maxZoom) / 2) // #nosec G115 -- min/maxZoom are bounded well within int range

	bounds := pkg.WorldBounds()
	if hasBounds {
		bounds = pkg.BoundsFromConfig(md.Bounds)
	}

	if hasCenter {
		lon, lat := md.Center[0], md.Center[centerLatIndex]
		bounds = pkg.Bounds{South: lat, North: lat, West: lon, East: lon}

		if len(md.Center) > centerZoomIndex {
			z = uint(md.Center[centerZoomIndex]) // #nosec G115 -- resolved metadata keeps the center zoom within the layer's zoom range
		}
	}

	zoomRange, err := seed.NewSingleZoomRange(bounds, z)
	if err != nil {
		return pkg.TileRequest{LayerName: layerName, Z: defaultZ, X: defaultX, Y: defaultY}
	}

	// A point on the east or south edge of the world lands one past the last tile
	lastTile := 1<<int(z) - 1                               // #nosec G115 -- z is at most MaxZoom
	x := min((zoomRange.XMin+zoomRange.XMax-1)/2, lastTile) //nolint:mnd // Midpoint of an exclusive-max range
	y := min((zoomRange.YMin+zoomRange.YMax-1)/2, lastTile) //nolint:mnd // Midpoint of an exclusive-max range

	return pkg.TileRequest{LayerName: layerName, Z: int(z), X: x, Y: y} // #nosec G115 -- z is 0-21, bounded well within int range
}

// A pattern layer's ID isn't a name, so it contributes its examples instead
func defaultLayerNames(layerObjects *layers.LayerGroup) []string {
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

func buildTileRequests(ctx context.Context, layerObjects *layers.LayerGroup, opts TestOptions) ([]pkg.TileRequest, error) {
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
	pkg.SetIdentity(ctx, opts.UserID, opts.TenantID)

	if opts.NumThread == 0 {
		return 0, errors.New("threads must be above 0")
	}

	ent, err := configToEntities(ctx, *cfg, nil)

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

	// In JSON mode with no file, stdout is reserved for the JSON summary
	tableOut := out
	if opts.JSON && opts.FilePath == "" {
		tableOut = io.Discard
	}

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

// Also writes to out in JSON mode without --file, since that's stdout's only output then
func writeSummary(out io.Writer, opts TestOptions, summary TestSummary) error {
	if opts.FilePath != "" {
		file, err := os.Create(opts.FilePath) // #nosec G304 -- operator-supplied path from the CLI, not user input
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

func testTileRequests(layerObjects *layers.LayerGroup, opts TestOptions, errCount *uint32, failuresMu *sync.Mutex, failures *[]TestFailure, writer *tabwriter.Writer, wg *sync.WaitGroup, t int, myReqs []pkg.TileRequest) {
	ctx := pkg.BackgroundContext()
	pkg.SetIdentity(ctx, opts.UserID, opts.TenantID)

	for _, req := range myReqs {
		var layerErr error
		var cacheWriteErr error
		var cacheReadErr error
		layer := layerObjects.FindLayer(ctx, req.LayerName)

		if layer == nil {
			layerErr = fmt.Errorf("layer %v unexpectedly not found", req.LayerName)
		} else {
			layerErr, cacheWriteErr, cacheReadErr = testTileRequest(ctx, layer, req, opts.NoCache)
		}

		allErrors := errors.Join(layerErr, cacheWriteErr, cacheReadErr)

		if allErrors != nil {
			atomic.AddUint32(errCount, 1)

			failuresMu.Lock()
			*failures = append(*failures, TestFailure{LayerName: req.LayerName, Error: allErrors.Error()})
			failuresMu.Unlock()
		}

		resultStr := strconv.Itoa(t) + "\t" + req.LayerName + "\t"
		if layerErr != nil {
			resultStr += "No\tN/A\tN/A\t\xff" + layerErr.Error() + "\xff\t"
		} else {
			if opts.NoCache { //nolint:gocritic
				resultStr += "Yes\tN/A\tN/A\tNone\t"
			} else if cacheWriteErr != nil {
				resultStr += "Yes\tNo\tN/A\t\xff" + cacheWriteErr.Error() + "\xff\t"
			} else if cacheReadErr != nil {
				resultStr += "Yes\tYes\tNo\t\xff" + cacheReadErr.Error() + "\xff\t"
			} else {
				resultStr += "Yes\tYes\tYes\tNone\t"
			}
		}
		fmt.Fprintln(writer, resultStr)

	}

	wg.Done()
}

// Recovers per request so one broken provider, script or cache can't abort the whole run
func testTileRequest(ctx context.Context, l *layers.Layer, req pkg.TileRequest, noCache bool) (error, error, error) {
	var layerErr error
	var cacheWriteErr error
	var cacheReadErr error

	stage := &layerErr

	defer func() {
		if r := recover(); r != nil {
			*stage = fmt.Errorf("panic: %v", r)
		}
	}()

	img, err := l.RenderTileNoCache(ctx, req)
	if err == nil && img == nil {
		err = errors.New("provider returned no image and no error")
	}

	layerErr = err
	if !noCache && err == nil {
		stage = &cacheWriteErr

		cacheWriteErr = l.Cache.Save(ctx, req, img)
		if cacheWriteErr == nil {

			stage = &cacheReadErr

			img2, err := l.Cache.Lookup(ctx, req)
			switch {
			case err != nil:
				cacheReadErr = err
			case img2 == nil:
				cacheReadErr = errors.New("no result from cache lookup")
			case !slices.Equal(img.Content, img2.Content):
				cacheReadErr = errors.New("cache result doesn't match what we put into cache")
			}
		}
	}

	return layerErr, cacheWriteErr, cacheReadErr
}
