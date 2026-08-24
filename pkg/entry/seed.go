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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

const (
	DefaultSeedMaxTiles = 1000000
	seedProgressVersion = 1
	seedOrderVersion    = "zoom-input/y-major/x-minor-v1"
)

var environmentReferencePattern = regexp.MustCompile(`env\.([A-Za-z_][A-Za-z0-9_]*)`)

type SeedOptions struct {
	Zoom      []uint
	Bounds    pkg.Bounds
	LayerName string
	Force     bool
	Verbose   bool
	NumThread uint16
}

type SeedRunOptions struct {
	SeedOptions
	MaxTiles         uint64
	ProgressFile     string
	ProgressRequired bool
}

type seedPlan struct {
	ranges []pkg.TileRange
	starts []uint64
	total  uint64
}

func newSeedPlan(opts SeedOptions) (seedPlan, error) {
	plan := seedPlan{
		ranges: make([]pkg.TileRange, 0, len(opts.Zoom)),
		starts: make([]uint64, 0, len(opts.Zoom)),
	}

	for _, zoom := range opts.Zoom {
		tileRange, err := opts.Bounds.TileRange(opts.LayerName, zoom)
		if err != nil {
			return seedPlan{}, err
		}

		count := tileRange.Count()
		if math.MaxUint64-plan.total < count {
			return seedPlan{}, errors.New("tile count exceeds supported range")
		}

		plan.starts = append(plan.starts, plan.total)
		plan.ranges = append(plan.ranges, tileRange)
		plan.total += count
	}

	return plan, nil
}

func (p seedPlan) At(index uint64) (pkg.TileRequest, bool) {
	if index >= p.total {
		return pkg.TileRequest{}, false
	}

	for i := len(p.ranges) - 1; i >= 0; i-- {
		if index >= p.starts[i] {
			return p.ranges[i].At(index - p.starts[i])
		}
	}

	return pkg.TileRequest{}, false
}

type seedProgress struct {
	Version   int       `json:"version"`
	Signature string    `json:"signature"`
	Order     string    `json:"order"`
	Next      uint64    `json:"next"`
	Total     uint64    `json:"total"`
	UpdatedAt time.Time `json:"updated_at"`
}

type seedSignatureInput struct {
	Order       string     `json:"order"`
	ConfigHash  string     `json:"config_hash"`
	Environment []string   `json:"environment"`
	LayerName   string     `json:"layer"`
	Zoom        []uint     `json:"zoom"`
	Bounds      pkg.Bounds `json:"bounds"`
}

func seedSignature(opts SeedOptions, cfg *config.Config) (string, error) {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	configHash := sha256.Sum256(configJSON)

	environment := referencedEnvironment(configJSON)
	encoded, err := json.Marshal(seedSignatureInput{
		Order:       seedOrderVersion,
		ConfigHash:  hex.EncodeToString(configHash[:]),
		Environment: environment,
		LayerName:   opts.LayerName,
		Zoom:        opts.Zoom,
		Bounds:      opts.Bounds,
	})
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func referencedEnvironment(configJSON []byte) []string {
	matches := environmentReferencePattern.FindAllSubmatch(configJSON, -1)
	values := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		name := string(match[1])
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		values = append(values, name+"="+os.Getenv(name))
	}
	slices.Sort(values)
	return values
}

func DefaultSeedProgressFile(cfg *config.Config, opts SeedOptions) (string, error) {
	signature, err := seedSignature(opts, cfg)
	if err != nil {
		return "", err
	}
	return ".tilegroxy-seed-" + signature[:16] + ".json", nil
}

func loadSeedProgress(path string, opts SeedOptions, cfg *config.Config, total uint64) (seedProgress, error) {
	progress := seedProgress{
		Version: seedProgressVersion,
		Order:   seedOrderVersion,
		Total:   total,
	}
	if path == "" {
		return progress, nil
	}

	signature, err := seedSignature(opts, cfg)
	if err != nil {
		return seedProgress{}, err
	}
	progress.Signature = signature

	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return progress, nil
	}
	if err != nil {
		return seedProgress{}, fmt.Errorf("read seed progress: %w", err)
	}

	var loaded seedProgress
	if err := json.Unmarshal(data, &loaded); err != nil {
		return seedProgress{}, fmt.Errorf("read seed progress: %w", err)
	}
	if loaded.Version != seedProgressVersion || loaded.Order != seedOrderVersion {
		return seedProgress{}, errors.New("seed progress uses an unsupported version; remove the progress file to restart")
	}
	if loaded.Signature != signature || loaded.Total != total {
		return seedProgress{}, errors.New("seed progress does not match this job; remove the progress file to restart")
	}
	if loaded.Next > total {
		return seedProgress{}, errors.New("seed progress is invalid; remove the progress file to restart")
	}

	return loaded, nil
}

func saveSeedProgress(path string, progress seedProgress) error {
	if path == "" {
		return nil
	}

	progress.UpdatedAt = time.Now().UTC()
	parent := filepath.Dir(path)
	tmp, err := os.CreateTemp(parent, ".tilegroxy-seed-progress-*")
	if err != nil {
		return fmt.Errorf("create seed progress: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck // Best-effort cleanup after rename or failure

	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(progress); err != nil {
		tmp.Close() //nolint:errcheck // Preserve the original error
		return fmt.Errorf("write seed progress: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck // Preserve the original error
		return fmt.Errorf("sync seed progress: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close seed progress: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Clean(path)); err != nil {
		return fmt.Errorf("replace seed progress: %w", err)
	}

	return nil
}

func removeSeedProgress(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(filepath.Clean(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove seed progress: %w", err)
	}
	return nil
}

type seedJob struct {
	ordinal uint64
	request pkg.TileRequest
}

type seedResult struct {
	thread  int
	ordinal uint64
	request pkg.TileRequest
	err     error
}

func Seed(cfg *config.Config, opts SeedOptions, out io.Writer) error {
	return SeedWithOptions(cfg, SeedRunOptions{
		SeedOptions: opts,
		MaxTiles:    DefaultSeedMaxTiles,
	}, out)
}

func SeedWithOptions(cfg *config.Config, opts SeedRunOptions, out io.Writer) error {
	ctx := pkg.BackgroundContext()
	if out == nil {
		out = io.Discard
	}

	if opts.NumThread == 0 {
		return errors.New("threads must be above 0")
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

	plan, err := newSeedPlan(opts.SeedOptions)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Estimated tiles: %v. Provider and cache operations depend on the layer configuration.\n", plan.total)
	if plan.total > opts.MaxTiles && !opts.Force {
		return fmt.Errorf("too many tiles to seed (%v > %v). Run with --force if you accept the estimated upstream and cache cost", plan.total, opts.MaxTiles)
	}

	progress, err := loadSeedProgress(opts.ProgressFile, opts.SeedOptions, cfg, plan.total)
	if err != nil {
		if !opts.ProgressRequired && progressUnavailable(err) {
			fmt.Fprintf(out, "Warning: progress tracking disabled: %v\n", err)
			opts.ProgressFile = ""
			progress, err = loadSeedProgress("", opts.SeedOptions, cfg, plan.total)
		}
		if err != nil {
			return err
		}
	}
	if progress.Next > 0 && progress.Next < plan.total {
		fmt.Fprintf(out, "Resuming at tile %v of %v\n", progress.Next+1, progress.Total)
	}
	if err := saveSeedProgress(opts.ProgressFile, progress); err != nil {
		if opts.ProgressRequired {
			return err
		}
		fmt.Fprintf(out, "Warning: progress tracking disabled: %v\n", err)
		opts.ProgressFile = ""
	}

	if progress.Next == plan.total {
		return finishSeed(opts, out)
	}

	runner := newSeedRunner(opts, out, layerGroup, plan, progress)
	if err := runner.run(); err != nil {
		return err
	}

	return finishSeed(opts, out)
}

func progressUnavailable(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.ENOTDIR)
}

func finishSeed(opts SeedRunOptions, out io.Writer) error {
	if err := removeSeedProgress(opts.ProgressFile); err != nil {
		return err
	}
	if opts.Verbose {
		fmt.Fprintln(out, "Completed seeding")
	}
	return nil
}

type seedRunner struct {
	opts         SeedRunOptions
	out          io.Writer
	layerGroup   *layer.LayerGroup
	plan         seedPlan
	progress     seedProgress
	jobs         chan seedJob
	results      chan seedResult
	wg           sync.WaitGroup
	window       uint64
	nextDispatch uint64
	inFlight     uint64
	completed    map[uint64]struct{}
	lastSaved    uint64
	lastSaveTime time.Time
	err          error
}

func newSeedRunner(opts SeedRunOptions, out io.Writer, layerGroup *layer.LayerGroup, plan seedPlan, progress seedProgress) *seedRunner {
	numThreads := uint64(opts.NumThread)
	remaining := plan.total - progress.Next
	if numThreads > remaining {
		fmt.Fprintln(os.Stderr, "Warning: more threads requested than tiles")
		numThreads = remaining
	}
	window := numThreads * 2

	return &seedRunner{
		opts:         opts,
		out:          out,
		layerGroup:   layerGroup,
		plan:         plan,
		progress:     progress,
		jobs:         make(chan seedJob, numThreads),
		results:      make(chan seedResult, window),
		window:       window,
		nextDispatch: progress.Next,
		completed:    make(map[uint64]struct{}, window),
		lastSaved:    progress.Next,
		lastSaveTime: time.Now(),
	}
}

func (r *seedRunner) run() error {
	r.startWorkers()
	r.dispatch()

	for r.inFlight > 0 {
		r.handleResult(<-r.results)
		r.dispatch()
	}

	close(r.jobs)
	r.wg.Wait()
	close(r.results)

	if r.err != nil {
		if err := r.saveFinalProgress(); err != nil {
			return errors.Join(r.err, err)
		}
		return r.err
	}
	if r.progress.Next != r.plan.total {
		if err := r.saveFinalProgress(); err != nil {
			return err
		}
		return fmt.Errorf("seeding stopped at tile %v of %v", r.progress.Next, r.plan.total)
	}

	return nil
}

func (r *seedRunner) saveFinalProgress() error {
	err := saveSeedProgress(r.opts.ProgressFile, r.progress)
	if err != nil && !r.opts.ProgressRequired && progressUnavailable(err) {
		fmt.Fprintf(r.out, "Warning: progress tracking disabled: %v\n", err)
		return nil
	}
	return err
}

func (r *seedRunner) startWorkers() {
	numThreads := cap(r.jobs)
	for thread := range numThreads {
		r.wg.Add(1)
		go seedWorker(&r.wg, thread, r.layerGroup, r.jobs, r.results)
		if r.opts.Verbose {
			fmt.Fprintf(r.out, "Created thread %v\n", thread)
		}
	}
}

func (r *seedRunner) dispatch() {
	for r.err == nil && r.nextDispatch-r.progress.Next < r.window && r.nextDispatch < r.plan.total {
		request, ok := r.plan.At(r.nextDispatch)
		if !ok {
			r.err = fmt.Errorf("unable to enumerate tile %v", r.nextDispatch)
			return
		}
		r.jobs <- seedJob{ordinal: r.nextDispatch, request: request}
		r.nextDispatch++
		r.inFlight++
	}
}

func (r *seedRunner) handleResult(result seedResult) {
	r.inFlight--
	r.writeResult(result)

	if result.err != nil {
		if r.err == nil {
			r.err = fmt.Errorf("tile %v failed: %w", result.request, result.err)
		}
	} else {
		r.completed[result.ordinal] = struct{}{}
		r.advanceProgress()
	}

	if r.progress.Next-r.lastSaved >= 1000 || time.Since(r.lastSaveTime) >= time.Second {
		r.checkpoint()
	}
}

func (r *seedRunner) checkpoint() {
	if err := saveSeedProgress(r.opts.ProgressFile, r.progress); err != nil {
		if !r.opts.ProgressRequired && progressUnavailable(err) {
			fmt.Fprintf(r.out, "Warning: progress tracking disabled: %v\n", err)
			r.opts.ProgressFile = ""
		} else if r.err == nil {
			r.err = err
		}
	}
	r.lastSaved = r.progress.Next
	r.lastSaveTime = time.Now()
}

func (r *seedRunner) writeResult(result seedResult) {
	if !r.opts.Verbose {
		return
	}

	status := "OK"
	if result.err != nil {
		status = result.err.Error()
	}
	fmt.Fprintf(r.out, "Thread %v - %v = %v\n", result.thread, result.request, status)
}

func (r *seedRunner) advanceProgress() {
	for {
		if _, ok := r.completed[r.progress.Next]; !ok {
			return
		}
		delete(r.completed, r.progress.Next)
		r.progress.Next++
	}
}

func seedWorker(wg *sync.WaitGroup, thread int, layerGroup *layer.LayerGroup, jobs <-chan seedJob, results chan<- seedResult) {
	defer wg.Done()

	for job := range jobs {
		result := seedResult{thread: thread, ordinal: job.ordinal, request: job.request}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					result.err = fmt.Errorf("worker panicked: %v", recovered)
				}
			}()
			_, result.err = layerGroup.RenderTileSync(pkg.BackgroundContext(), job.request)
		}()
		results <- result
	}
}
