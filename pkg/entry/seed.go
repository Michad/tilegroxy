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
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal/seed"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

// warnCount is how many tiles a run can cover before it needs to be confirmed with Force. Seeding is
// streamed so the number isn't a memory limit, it's the point where a run is big enough to be worth
// a second look before it spends hours against an upstream provider.
const warnCount = 100000

// assumedTilesPerSecond is the throughput a single thread is assumed to manage when estimating how
// long a run takes. Deliberately pessimistic since the estimate only exists to give the excessive
// tile count a sense of scale.
const assumedTilesPerSecond = 5

// progressInterval is how often progress is written to disk. Every tile would be a write per tile,
// nothing at all would lose the whole run, so a run killed at any point loses at most this much
// work.
const progressInterval = 100

type SeedOptions struct {
	Zoom         []uint
	Bounds       pkg.Bounds
	LayerName    string
	Force        bool
	Verbose      bool
	NumThread    uint16
	Resume       bool   // Continue an interrupted run from the position in its progress file
	ProgressFile string // Where progress is recorded. Empty disables progress tracking entirely
}

// DefaultProgressPath is where a seed run records its progress when no location is given: next to
// the configuration file, named for the layer being seeded.
func DefaultProgressPath(configPath, layerName string) string {
	return seed.DefaultProgressPath(configPath, layerName)
}

func Seed(cfg *config.Config, opts SeedOptions, out io.Writer) error {
	ctx := pkg.BackgroundContext()

	if opts.NumThread == 0 {
		return errors.New("threads must be above 0")
	}

	enumeration, err := seed.NewEnumeration(opts.LayerName, opts.Bounds, opts.Zoom)
	if err != nil {
		return err
	}

	if err = checkSeedSize(enumeration, opts, out); err != nil {
		return err
	}

	progress, start, err := resolveProgress(enumeration, opts, out)
	if err != nil {
		return err
	}

	ent, err := configToEntities(*cfg)
	if err != nil {
		return err
	}

	defer ent.Close(ctx) //nolint:errcheck // Nothing actionable while tearing down a seed run

	layerGroup := ent.LayerGroup

	if layerGroup.FindLayer(ctx, opts.LayerName) == nil {
		return errors.New("invalid layer")
	}

	if opts.Verbose {
		fmt.Fprintf(out, "Number of tile requests: %v\n", enumeration.Count()-start)
	}

	numThread := opts.NumThread
	if remaining := enumeration.Count() - start; remaining > 0 && uint64(numThread) > remaining {
		fmt.Fprintln(out, "Warning: more threads requested than tiles")

		numThread = uint16(remaining) // #nosec G115 -- guarded by the comparison above
	}

	if err = seedTiles(enumeration, opts, out, layerGroup, numThread, progress, start); err != nil {
		return err
	}

	if progress != nil {
		if err = progress.Save(opts.ProgressFile); err != nil {
			return err
		}
	}

	if opts.Verbose {
		fmt.Fprintf(out, "Completed seeding")
	}

	return nil
}

// checkSeedSize stops a run that covers an excessive number of tiles unless it's been confirmed with
// Force. The count comes from the bounds rather than from enumerating, so this costs nothing even
// for a run covering millions of tiles.
func checkSeedSize(enumeration *seed.Enumeration, opts SeedOptions, out io.Writer) error {
	count := enumeration.Count()

	if count <= warnCount || opts.Force {
		if opts.Verbose && count > warnCount {
			fmt.Fprintf(out, "Seeding %v tiles, an estimated %v\n", count, estimateDuration(count, opts.NumThread))
		}

		return nil
	}

	return fmt.Errorf("seeding %v tiles takes an estimated %v against the upstream provider, above the %v tile threshold. Run with --force if you're sure you want to generate this many tiles",
		count, estimateDuration(count, opts.NumThread), warnCount)
}

// estimateDuration is a rough sense of how long a run takes, to give the tile count some meaning.
// The real figure depends entirely on the provider so this is only ever an order of magnitude.
func estimateDuration(count uint64, numThread uint16) time.Duration {
	perSecond := uint64(numThread) * assumedTilesPerSecond

	return (time.Duration(count/perSecond) * time.Second).Round(time.Minute)
}

// resolveProgress works out where the run starts and what it records progress to. A progress file
// left by a run of a different shape is refused rather than resumed or quietly overwritten, since
// its position refers to tiles this run doesn't cover.
func resolveProgress(enumeration *seed.Enumeration, opts SeedOptions, out io.Writer) (*seed.Progress, uint64, error) {
	if opts.ProgressFile == "" {
		return nil, 0, nil
	}

	existing, err := seed.LoadProgress(opts.ProgressFile)
	if err != nil {
		return nil, 0, err
	}

	if existing == nil {
		return seed.NewProgress(opts.LayerName, enumeration), 0, nil
	}

	if !existing.Matches(opts.LayerName, enumeration) {
		return nil, 0, fmt.Errorf("%w: %v covers a different layer, area or zoom range. Delete it or seed with the arguments it records", seed.ErrProgressMismatch, opts.ProgressFile)
	}

	if !opts.Resume {
		if existing.Position > 0 {
			fmt.Fprintf(out, "Warning: %v records %v of %v tiles already seeded. Run with --resume to continue from there instead of starting over\n", opts.ProgressFile, existing.Position, existing.Total)
		}

		return seed.NewProgress(opts.LayerName, enumeration), 0, nil
	}

	if opts.Verbose && existing.Position > 0 {
		fmt.Fprintf(out, "Resuming from tile %v of %v\n", existing.Position, existing.Total)
	}

	return existing, existing.Position, nil
}

// seedTiles renders every tile of the run from start onwards. Tiles are pulled off a channel fed by
// the enumeration so only the tiles currently in flight are ever in memory, and only NumThread of
// them are rendered at a time no matter how big the run is.
func seedTiles(enumeration *seed.Enumeration, opts SeedOptions, out io.Writer, layerGroup *layer.LayerGroup, numThread uint16, progress *seed.Progress, start uint64) error {
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

		trackErr = trackProgress(opts, progress, start, done)
	}()

feed:
	for i, req := range enumeration.From(start) {
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

	// A panicking thread skipped its tile. Reporting that to `out` isn't enough: with no error the
	// command exits 0 and a partial seed looks like a complete one.
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

// indexedTile carries a tile along with its position in the enumeration so progress can be recorded
// against the sequence rather than against a count of finished tiles.
type indexedTile struct {
	request pkg.TileRequest
	index   uint64
}

// trackProgress records how far the run got. Threads finish out of order, so the position only
// advances past a tile once every tile before it is done, meaning a resumed run never skips one.
func trackProgress(opts SeedOptions, progress *seed.Progress, start uint64, done <-chan uint64) error {
	next := start
	pending := make(map[uint64]struct{})
	sinceSave := 0

	// A failed save is reported once the channel has drained rather than returned straight away, so
	// the threads still feeding it never block on a tracker that stopped listening.
	var saveErr error

	for i := range done {
		pending[i] = struct{}{}

		for {
			if _, ok := pending[next]; !ok {
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

			saveErr = progress.Save(opts.ProgressFile)
		}
	}

	if progress != nil {
		progress.Position = next
	}

	return saveErr
}

// seedThread renders tiles until the channel is drained. A panic, e.g. from a buggy custom provider,
// is recovered so the other threads can finish, and reported on errs since the tile it was rendering
// never completed.
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

	for tile := range tiles {
		_, tileErr := layerGroup.RenderTile(pkg.BackgroundContext(), tile.request)

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
