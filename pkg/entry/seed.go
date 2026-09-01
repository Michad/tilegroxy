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
	"strings"
	"sync"

	"github.com/Michad/tilegroxy/internal/seed"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

// how many tiles a run can cover before it needs to be confirmed with Force.
const warnCount = 10000

// how often progress is written to disk
const progressInterval = 100

type SeedOptions struct {
	Zoom         []uint
	Bounds       pkg.Bounds
	LayerName    string
	Force        bool
	Verbose      bool
	NumThread    uint16
	ProgressFile string
	CacheName    string
}

func Seed(cfg *config.Config, opts SeedOptions, out io.Writer) error {
	ctx := pkg.BackgroundContext()

	if opts.NumThread == 0 {
		return errors.New("threads must be above 0")
	}

	seedJob, err := seed.NewSeedJob(opts.LayerName, opts.Bounds, opts.Zoom)
	if err != nil {
		return err
	}

	if err = checkSeedSize(seedJob, opts, out); err != nil {
		return err
	}

	progress, start, err := resolveProgress(seedJob, opts, out)
	if err != nil {
		return err
	}

	entityConfig := *cfg

	if opts.CacheName != "" {
		entityConfig.Cache, err = restrictToCacheTier(entityConfig.Cache, opts.CacheName)
		if err != nil {
			return err
		}
	}

	ent, err := configToEntities(entityConfig)
	if err != nil {
		return err
	}

	defer ent.Close(ctx) //nolint:errcheck // Nothing actionable while tearing down a seed run

	layerGroup := ent.LayerGroup

	if layerGroup.FindLayer(ctx, opts.LayerName) == nil {
		return errors.New("invalid layer")
	}

	if opts.Verbose {
		fmt.Fprintf(out, "Number of tile requests: %v\n", seedJob.Count()-start)
	}

	numThread := opts.NumThread
	if remaining := seedJob.Count() - start; remaining > 0 && uint64(numThread) > remaining {
		fmt.Fprintln(out, "Warning: more threads requested than tiles")

		numThread = uint16(remaining) // #nosec G115 -- guarded by the comparison above
	}

	if err = seedTiles(seedJob, opts, out, layerGroup, numThread, progress, start); err != nil {
		return err
	}

	if progress != nil {
		if err = progress.Finish(opts.ProgressFile, out, opts.Verbose); err != nil {
			return err
		}
	}

	if opts.Verbose {
		fmt.Fprintf(out, "Completed seeding")
	}

	return nil
}

// rewrites raw cache config so only the named tier of a "multi" cache gets constructed
func restrictToCacheTier(rawConfig map[string]interface{}, name string) (map[string]interface{}, error) {
	cacheName, _ := rawConfig["name"].(string)
	if !strings.EqualFold(cacheName, "multi") {
		return nil, fmt.Errorf("cache %q not found: layer's cache is not multi-tiered", name)
	}

	var matched map[string]interface{}

	switch rawTiers := rawConfig["tiers"].(type) {
	case []map[string]interface{}:
		for _, tier := range rawTiers {
			if tierName, _ := tier["name"].(string); strings.EqualFold(tierName, name) {
				matched = tier
				break
			}
		}
	case []interface{}:
		for _, rawTier := range rawTiers {
			tier, ok := rawTier.(map[string]interface{})
			if !ok {
				continue
			}

			if tierName, _ := tier["name"].(string); strings.EqualFold(tierName, name) {
				matched = tier
				break
			}
		}
	}

	if matched == nil {
		return nil, fmt.Errorf("cache %q not found: no matching tier in the multi cache", name)
	}

	return matched, nil
}

func checkSeedSize(seedJob *seed.SeedJob, opts SeedOptions, out io.Writer) error {
	count := seedJob.Count()

	if count <= warnCount || opts.Force && count < math.MaxInt32 {
		if opts.Verbose && count > warnCount {
			fmt.Fprintf(out, "Seeding %v tiles\n", count)
		}

		return nil
	}

	return fmt.Errorf("too many tiles to seed (%v > %v). %v",
		count,
		pkg.Ternary(count > math.MaxInt32, math.MaxInt32, warnCount),
		pkg.Ternary(count > math.MaxInt32, "", "Run with --force if you're sure you want to generate this many tiles"))
}

func resolveProgress(currentSeedJob *seed.SeedJob, opts SeedOptions, out io.Writer) (*seed.Progress, uint64, error) {
	if opts.ProgressFile == "" {
		return nil, 0, nil
	}

	existing, err := seed.LoadProgress(opts.ProgressFile)
	if err != nil {
		return nil, 0, err
	}

	if existing == nil {
		return seed.NewProgress(opts.LayerName, currentSeedJob), 0, nil
	}

	if !existing.Matches(opts.LayerName, currentSeedJob) {
		return nil, 0, fmt.Errorf("%w: %v covers a different layer, area or zoom range. Delete it or seed with the arguments it records", seed.ErrProgressMismatch, opts.ProgressFile)
	}

	if opts.Verbose && existing.Position > 0 {
		fmt.Fprintf(out, "Resuming from tile %v of %v\n", existing.Position, existing.Total)
	}

	return existing, existing.Position, nil
}

func seedTiles(seedJob *seed.SeedJob, opts SeedOptions, out io.Writer, layerGroup *layer.LayerGroup, numThread uint16, progress *seed.Progress, start uint64) error {
	tiles := make(chan indexedTile)
	done := make(chan uint64, numThread)

	var wg sync.WaitGroup

	// Buffered per thread so a panicking thread never blocks on the send.
	errs := make(chan error, numThread)

	// Closed once every thread has stopped, so the feed below doesn't block forever handing out
	// tiles when the threads have all panicked and nothing is left to render them.
	abandoned := make(chan struct{})

	for t := range int(numThread) {
		wg.Add(1)

		go seedThread(&wg, opts, out, layerGroup, t, tiles, done, errs)
	}

	go func() {
		wg.Wait()
		close(abandoned)
	}()

	// A single writer owns the progress file, so worker threads never touch it concurrently.
	var trackerWg sync.WaitGroup

	trackerWg.Add(1)

	var trackErr error

	go func() {
		defer trackerWg.Done()

		trackErr = trackProgress(opts, progress, start, done, out)
	}()

feed:
	for i, req := range seedJob.From(start) {
		select {
		case tiles <- indexedTile{index: i, request: req}:
		case <-abandoned:
			break feed
		}
	}

	close(tiles)
	wg.Wait()
	close(done)
	trackerWg.Wait()
	close(errs)

	// A panicking thread skipped its tile.
	var threadErrs []error
	for err := range errs {
		threadErrs = append(threadErrs, err)
	}

	if trackErr != nil {
		threadErrs = append(threadErrs, trackErr)
	}

	if len(threadErrs) > 0 {
		return errors.Join(threadErrs...)
	}

	return nil
}

type indexedTile struct {
	request pkg.TileRequest
	index   uint64
}

func trackProgress(opts SeedOptions, progress *seed.Progress, start uint64, done <-chan uint64, out io.Writer) error {
	next := start
	pending := make(map[uint64]struct{})
	sinceSave := 0

	var saveErr error

	for i := range done {
		// Which ones have finished
		pending[i] = struct{}{}

		for {
			if _, ok := pending[next]; !ok {
				// Make sure they're recorded in order
				// e.g. actual seeded: OOO -> XOO -> XOX -> XXX
				// what we record:      0  ->  1  ->  1  ->  3
				// If we fail in step 3, we'll end up re-seeding group 3, but won't miss any
				break
			}

			delete(pending, next)
			next++
		}

		if progress == nil || saveErr != nil {
			continue
		}

		sinceSave++
		if sinceSave >= progressInterval && next > progress.Position {
			progress.Position = next
			sinceSave = 0

			saveErr = progress.Save(opts.ProgressFile, out, opts.Verbose)
		}
	}

	if progress != nil {
		progress.Position = next
	}

	return saveErr
}

func seedThread(wg *sync.WaitGroup, opts SeedOptions, out io.Writer, layerGroup *layer.LayerGroup, t int, tiles <-chan indexedTile, done chan<- uint64, errs chan<- error) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(out, "Thread %v panicked: %v\n", t, r)
			errs <- fmt.Errorf("thread %v panicked: %v", t, r)
		}
	}()

	if opts.Verbose {
		fmt.Fprintf(out, "Created thread %v\n", t)
	}

	ctx := pkg.BackgroundContext()

	for tile := range tiles {
		_, tileErr := layerGroup.RenderTile(ctx, tile.request)

		if opts.Verbose {
			var status string
			if tileErr == nil {
				status = "OK"
			} else {
				status = tileErr.Error()
			}

			fmt.Fprintf(out, "Thread %v - %v = %v\n", t, tile.request, status)
		}

		done <- tile.index
	}

	if opts.Verbose {
		fmt.Fprintf(out, "Finished thread %v\n", t)
	}
}
