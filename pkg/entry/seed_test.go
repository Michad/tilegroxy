// Copyright 2026 Michael Davis
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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/seed"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seedTestPanicProvider struct{}

func (seedTestPanicProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (seedTestPanicProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	panic("simulated panic from a buggy provider")
}

type seedTestPanicRegistration struct{}

func (seedTestPanicRegistration) Name() string                   { return "seed-test-panic-provider" }
func (seedTestPanicRegistration) InitializeConfig() any          { return struct{}{} }
func (seedTestPanicRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }
func (seedTestPanicRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return seedTestPanicProvider{}, nil
}

// seedThread runs on its own goroutine per chunk of tiles, so an unrecovered panic there, e.g.
// from a buggy custom provider, crashes the entire seed process.
func Test_SeedThread_RecoversFromPanic(t *testing.T) {
	layer.RegisterProvider(seedTestPanicRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "panics", Provider: map[string]interface{}{"name": "seed-test-panic-provider"}},
	}

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var out bytes.Buffer
	wg.Add(1)
	errs := make(chan error, 1)
	tiles := make(chan indexedTile, 1)
	done := make(chan uint64, 1)
	tiles <- indexedTile{request: pkg.TileRequest{LayerName: "panics", Z: 1, X: 0, Y: 0}}
	close(tiles)

	require.NotPanics(t, func() {
		seedThread(&wg, SeedOptions{Verbose: true}, &out, lg, 0, tiles, done, errs)
	})

	wg.Wait()
	close(errs)
	require.Contains(t, out.String(), "panicked")

	// The thread abandoned its tile, so recovering must not swallow the panic.
	require.Len(t, errs, 1)
	require.ErrorContains(t, <-errs, "panicked")
}

// A panicking thread skips a whole chunk of tiles. Without an error the command exits 0 and a
// partial seed is indistinguishable from a complete one.
func Test_Seed_ReturnsErrorWhenThreadPanics(t *testing.T) {
	layer.RegisterProvider(seedTestPanicRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "panics", Provider: map[string]interface{}{"name": "seed-test-panic-provider"}},
	}

	var out bytes.Buffer
	err := Seed(&cfg, SeedOptions{
		Zoom:      []uint{1},
		Bounds:    pkg.Bounds{South: -10, North: 10, West: -10, East: 10},
		LayerName: "panics",
		NumThread: 1,
	}, &out)

	require.Error(t, err)
	require.ErrorContains(t, err, "panicked")
}

// seedTestCountingProvider records how many renders are in flight at once so the thread limit can be
// checked, and how many tiles were rendered in total.
type seedTestCountingProvider struct {
	mutex    sync.Mutex
	inFlight int
	maxSeen  int
	rendered []pkg.TileRequest
}

var seedTestCounter = &seedTestCountingProvider{}

func (p *seedTestCountingProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (p *seedTestCountingProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, req pkg.TileRequest) (*pkg.Image, error) {
	p.mutex.Lock()
	p.inFlight++
	if p.inFlight > p.maxSeen {
		p.maxSeen = p.inFlight
	}
	p.rendered = append(p.rendered, req)
	p.mutex.Unlock()

	// Long enough that concurrent renders actually overlap, so the in flight count is meaningful.
	time.Sleep(50 * time.Microsecond)

	p.mutex.Lock()
	p.inFlight--
	p.mutex.Unlock()

	return &pkg.Image{Content: []byte{}, ContentType: "image/png"}, nil
}

func (p *seedTestCountingProvider) reset() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.inFlight = 0
	p.maxSeen = 0
	p.rendered = nil
}

func (p *seedTestCountingProvider) snapshot() (int, []pkg.TileRequest) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.maxSeen, slices.Clone(p.rendered)
}

type seedTestCountingRegistration struct{}

func (seedTestCountingRegistration) Name() string                   { return "seed-test-counting-provider" }
func (seedTestCountingRegistration) InitializeConfig() any          { return struct{}{} }
func (seedTestCountingRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }
func (seedTestCountingRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return seedTestCounter, nil
}

func seedTestConfig(t *testing.T) config.Config {
	t.Helper()
	layer.RegisterProvider(seedTestCountingRegistration{})
	seedTestCounter.reset()

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "counts", Provider: map[string]interface{}{"name": "seed-test-counting-provider"}},
	}

	return cfg
}

// Streaming the tiles must not make the seed unboundedly parallel; --threads is what keeps a seed
// from hammering an upstream provider.
func Test_Seed_RespectsThreadLimit(t *testing.T) {
	cfg := seedTestConfig(t)

	var out bytes.Buffer
	require.NoError(t, Seed(&cfg, SeedOptions{
		Zoom:      []uint{0, 1, 2, 3},
		Bounds:    pkg.WorldBounds(),
		LayerName: "counts",
		NumThread: 3,
	}, &out))

	maxSeen, rendered := seedTestCounter.snapshot()
	assert.LessOrEqual(t, maxSeen, 3)
	assert.Len(t, rendered, 1+4+16+64)
}

func Test_Seed_MoreThreadsThanTiles(t *testing.T) {
	cfg := seedTestConfig(t)

	var out bytes.Buffer
	require.NoError(t, Seed(&cfg, SeedOptions{
		Zoom:      []uint{0},
		Bounds:    pkg.WorldBounds(),
		LayerName: "counts",
		NumThread: 8,
	}, &out))

	maxSeen, rendered := seedTestCounter.snapshot()
	assert.LessOrEqual(t, maxSeen, 1)
	assert.Len(t, rendered, 1)
	assert.Contains(t, out.String(), "more threads requested than tiles")
}

func Test_Seed_ZeroThreads(t *testing.T) {
	cfg := seedTestConfig(t)

	var out bytes.Buffer
	require.ErrorContains(t, Seed(&cfg, SeedOptions{
		Zoom:      []uint{0},
		LayerName: "counts",
		NumThread: 0,
	}, &out), "threads")
}

func Test_Seed_InvalidZoom(t *testing.T) {
	cfg := seedTestConfig(t)

	var out bytes.Buffer
	require.Error(t, Seed(&cfg, SeedOptions{
		Zoom:      []uint{pkg.MaxZoom + 1},
		LayerName: "counts",
		NumThread: 1,
	}, &out))
}

func Test_Seed_InvalidLayer(t *testing.T) {
	cfg := seedTestConfig(t)

	var out bytes.Buffer
	require.ErrorContains(t, Seed(&cfg, SeedOptions{
		Zoom:      []uint{0},
		Bounds:    pkg.WorldBounds(),
		LayerName: "nope",
		NumThread: 1,
	}, &out), "invalid layer")
}

// The guard is now a count of how many tiles the run covers, not a memory ceiling, and --force is
// still the way to say you meant it.
func Test_Seed_ExcessiveTileCountNeedsForce(t *testing.T) {
	cfg := seedTestConfig(t)
	opts := SeedOptions{
		Zoom:      []uint{10},
		Bounds:    pkg.WorldBounds(),
		LayerName: "counts",
		NumThread: 1,
	}

	var out bytes.Buffer
	err := Seed(&cfg, opts, &out)
	require.ErrorContains(t, err, "--force")

	maxSeen, rendered := seedTestCounter.snapshot()
	assert.Equal(t, 0, maxSeen)
	assert.Empty(t, rendered)
}

// A run over the threshold goes ahead once it's been confirmed. Checked against the guard rather
// than by seeding, since actually rendering a run that size is the thing --force exists to allow.
func Test_Seed_ForceAllowsExcessiveTileCount(t *testing.T) {
	e, err := seed.NewSeedJob("counts", pkg.WorldBounds(), []uint{10})
	require.NoError(t, err)
	require.Greater(t, e.Count(), uint64(warnCount))

	var out bytes.Buffer
	opts := SeedOptions{LayerName: "counts", Force: true, NumThread: 4, Verbose: true}

	require.NoError(t, checkSeedSize(e, opts, &out))
}

func Test_Seed_WritesProgressFile(t *testing.T) {
	cfg := seedTestConfig(t)
	path := filepath.Join(t.TempDir(), "progress.json")

	var out bytes.Buffer
	require.NoError(t, Seed(&cfg, SeedOptions{
		Zoom:         []uint{0, 1, 2},
		Bounds:       pkg.WorldBounds(),
		LayerName:    "counts",
		NumThread:    2,
		ProgressFile: path,
	}, &out))

	progress, err := seed.LoadProgress(path)
	require.NoError(t, err)
	require.NotNil(t, progress)
	assert.Equal(t, uint64(1+4+16), progress.Position)
	assert.Equal(t, progress.Total, progress.Position)
}

// Resuming has to pick up at exactly the recorded position, seeding the rest of the sequence and
// nothing before it.
func Test_Seed_ResumesFromRecordedPosition(t *testing.T) {
	cfg := seedTestConfig(t)
	path := filepath.Join(t.TempDir(), "progress.json")

	e, err := seed.NewSeedJob("counts", pkg.WorldBounds(), []uint{0, 1, 2})
	require.NoError(t, err)

	partial := seed.NewProgress("counts", e)
	partial.Position = 5
	require.NoError(t, partial.Save(path))

	var out bytes.Buffer
	require.NoError(t, Seed(&cfg, SeedOptions{
		Zoom:         []uint{0, 1, 2},
		Bounds:       pkg.WorldBounds(),
		LayerName:    "counts",
		NumThread:    1,
		Verbose:      true,
		ProgressFile: path,
	}, &out))

	_, rendered := seedTestCounter.snapshot()
	require.Len(t, rendered, int(e.Count())-5)
	assert.Equal(t, pkg.TileRequest{LayerName: "counts", Z: 2, X: 0, Y: 0}, rendered[0])
	assert.Contains(t, out.String(), "Resuming from tile 5")

	progress, err := seed.LoadProgress(path)
	require.NoError(t, err)
	assert.Equal(t, e.Count(), progress.Position)
}

// Resuming a completed run has nothing left to do rather than seeding the whole thing again.
func Test_Seed_ResumeFromCompletedRun(t *testing.T) {
	cfg := seedTestConfig(t)
	path := filepath.Join(t.TempDir(), "progress.json")

	e, err := seed.NewSeedJob("counts", pkg.WorldBounds(), []uint{0, 1})
	require.NoError(t, err)

	finished := seed.NewProgress("counts", e)
	finished.Position = e.Count()
	require.NoError(t, finished.Save(path))

	var out bytes.Buffer
	require.NoError(t, Seed(&cfg, SeedOptions{
		Zoom:         []uint{0, 1},
		Bounds:       pkg.WorldBounds(),
		LayerName:    "counts",
		NumThread:    4,
		ProgressFile: path,
	}, &out))

	_, rendered := seedTestCounter.snapshot()
	assert.Empty(t, rendered)
}

// A progress file for a different area indexes into a different sequence, so resuming from it would
// seed the wrong tiles. That has to fail loudly rather than silently restarting.
func Test_Seed_RefusesMismatchedProgressFile(t *testing.T) {
	cfg := seedTestConfig(t)
	path := filepath.Join(t.TempDir(), "progress.json")

	e, err := seed.NewSeedJob("counts", pkg.Bounds{South: 10, North: 20, West: 10, East: 20, SRID: pkg.SRIDWGS84}, []uint{0, 1, 2})
	require.NoError(t, err)

	other := seed.NewProgress("counts", e)
	other.Position = 3
	require.NoError(t, other.Save(path))

	var out bytes.Buffer
	err = Seed(&cfg, SeedOptions{
		Zoom:         []uint{0, 1, 2},
		Bounds:       pkg.WorldBounds(),
		LayerName:    "counts",
		NumThread:    1,
		ProgressFile: path,
	}, &out)

	require.ErrorIs(t, err, seed.ErrProgressMismatch)

	_, rendered := seedTestCounter.snapshot()
	assert.Empty(t, rendered)
}

func Test_Seed_CorruptProgressFile(t *testing.T) {
	cfg := seedTestConfig(t)
	path := filepath.Join(t.TempDir(), "progress.json")
	require.NoError(t, os.WriteFile(path, []byte("{{{"), 0600))

	var out bytes.Buffer
	require.Error(t, Seed(&cfg, SeedOptions{
		Zoom:         []uint{0},
		Bounds:       pkg.WorldBounds(),
		LayerName:    "counts",
		NumThread:    1,
		ProgressFile: path,
	}, &out))
}

func Test_Seed_UnwritableProgressFile(t *testing.T) {
	cfg := seedTestConfig(t)

	var out bytes.Buffer
	require.Error(t, Seed(&cfg, SeedOptions{
		Zoom:         []uint{0},
		Bounds:       pkg.WorldBounds(),
		LayerName:    "counts",
		NumThread:    1,
		ProgressFile: filepath.Join(t.TempDir(), "no-such-dir", "progress.json"),
	}, &out))
}

// Progress is saved periodically during a run, so a seed killed partway through leaves usable state
// rather than nothing at all.
func Test_Seed_SavesProgressDuringRun(t *testing.T) {
	cfg := seedTestConfig(t)
	path := filepath.Join(t.TempDir(), "progress.json")

	var out bytes.Buffer
	require.NoError(t, Seed(&cfg, SeedOptions{
		Zoom:         []uint{0, 1, 2, 3, 4},
		Bounds:       pkg.WorldBounds(),
		LayerName:    "counts",
		NumThread:    4,
		ProgressFile: path,
	}, &out))

	progress, err := seed.LoadProgress(path)
	require.NoError(t, err)
	assert.Equal(t, uint64(1+4+16+64+256), progress.Position)
}
