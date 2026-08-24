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
	"errors"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
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

// seedWorker runs in its own goroutine, so an unrecovered panic there, e.g. from a buggy custom
// provider, crashes the entire seed process.
func Test_SeedWorker_RecoversFromPanic(t *testing.T) {
	layer.RegisterProvider(seedTestPanicRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "panics", Provider: map[string]interface{}{"name": "seed-test-panic-provider"}},
	}

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	var wg sync.WaitGroup
	jobs := make(chan seedJob, 1)
	results := make(chan seedResult, 1)
	wg.Add(1)
	jobs <- seedJob{ordinal: 0, request: pkg.TileRequest{LayerName: "panics", Z: 1, X: 0, Y: 0}}
	close(jobs)

	require.NotPanics(t, func() {
		seedWorker(&wg, 0, lg, jobs, results)
	})

	wg.Wait()
	close(results)
	result := <-results
	require.Error(t, result.err)
	require.ErrorContains(t, result.err, "panicked")
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
	err := SeedWithOptions(&cfg, SeedRunOptions{
		SeedOptions: SeedOptions{
			Zoom:      []uint{1},
			Bounds:    pkg.Bounds{South: -10, North: 10, West: -10, East: 10},
			LayerName: "panics",
			NumThread: 1,
		},
		MaxTiles:     DefaultSeedMaxTiles,
		ProgressFile: t.TempDir() + "/progress.json",
	}, &out)

	require.Error(t, err)
	require.ErrorContains(t, err, "panicked")
}

type seedTestResumeProvider struct{}

var seedTestResumeCalls = map[string]int{}
var seedTestResumeFailRequest string
var seedTestResumeShouldFail bool
var seedTestResumeMu sync.Mutex

func (seedTestResumeProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (seedTestResumeProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, request pkg.TileRequest) (*pkg.Image, error) {
	seedTestResumeMu.Lock()
	defer seedTestResumeMu.Unlock()

	key := request.String()
	seedTestResumeCalls[key]++
	if seedTestResumeShouldFail && key == seedTestResumeFailRequest {
		seedTestResumeShouldFail = false
		return nil, errors.New("simulated render failure")
	}
	return &pkg.Image{Content: []byte(key)}, nil
}

type seedTestResumeRegistration struct{}

func (seedTestResumeRegistration) Name() string                   { return "seed-test-resume-provider" }
func (seedTestResumeRegistration) InitializeConfig() any          { return struct{}{} }
func (seedTestResumeRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }
func (seedTestResumeRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return seedTestResumeProvider{}, nil
}

func seedTestResumeConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "resume", Provider: map[string]interface{}{"name": "seed-test-resume-provider"}},
	}
	return cfg
}

func Test_Seed_ResumesFromDurablePrefix(t *testing.T) {
	layer.RegisterProvider(seedTestResumeRegistration{})

	seedTestResumeMu.Lock()
	seedTestResumeCalls = map[string]int{}
	seedTestResumeFailRequest = "resume/1/0/1"
	seedTestResumeShouldFail = true
	seedTestResumeMu.Unlock()

	cfg := seedTestResumeConfig()
	progressFile := t.TempDir() + "/seed-progress.json"
	opts := SeedRunOptions{
		SeedOptions: SeedOptions{
			Zoom:      []uint{1},
			Bounds:    pkg.Bounds{South: -90, North: 90, West: -180, East: 180},
			LayerName: "resume",
			NumThread: 1,
		},
		MaxTiles:     DefaultSeedMaxTiles,
		ProgressFile: progressFile,
	}

	var out bytes.Buffer
	err := SeedWithOptions(&cfg, opts, &out)
	require.ErrorContains(t, err, "simulated render failure")

	progress, err := loadSeedProgress(progressFile, opts.SeedOptions, &cfg, 4)
	require.NoError(t, err)
	require.Equal(t, uint64(2), progress.Next)

	out.Reset()
	require.NoError(t, SeedWithOptions(&cfg, opts, &out))
	require.Contains(t, out.String(), "Resuming at tile 3 of 4")
	require.NoFileExists(t, progressFile)

	seedTestResumeMu.Lock()
	defer seedTestResumeMu.Unlock()
	require.Equal(t, 1, seedTestResumeCalls["resume/1/0/0"])
	require.Equal(t, 1, seedTestResumeCalls["resume/1/1/0"])
	require.Equal(t, 2, seedTestResumeCalls["resume/1/0/1"])
}

func Test_Seed_UsesExactCostGuard(t *testing.T) {
	layer.RegisterProvider(seedTestResumeRegistration{})

	seedTestResumeMu.Lock()
	seedTestResumeCalls = map[string]int{}
	seedTestResumeShouldFail = false
	seedTestResumeMu.Unlock()

	cfg := seedTestResumeConfig()
	var out bytes.Buffer
	err := SeedWithOptions(&cfg, SeedRunOptions{
		SeedOptions: SeedOptions{
			Zoom:      []uint{1},
			Bounds:    pkg.Bounds{South: -90, North: 90, West: -180, East: 180},
			LayerName: "resume",
			NumThread: 1,
		},
		MaxTiles: 3,
	}, &out)

	require.ErrorContains(t, err, "too many tiles to seed (4 > 3)")
	require.Contains(t, out.String(), "Estimated tiles: 4")
	seedTestResumeMu.Lock()
	defer seedTestResumeMu.Unlock()
	require.Empty(t, seedTestResumeCalls)
}

func Test_LoadSeedProgress_RejectsDifferentJob(t *testing.T) {
	path := t.TempDir() + "/seed-progress.json"
	opts := SeedOptions{Zoom: []uint{1}, Bounds: pkg.Bounds{South: -1, North: 1, West: -1, East: 1}, LayerName: "one"}
	cfg := seedTestResumeConfig()
	progress, err := loadSeedProgress(path, opts, &cfg, 4)
	require.NoError(t, err)
	require.NoError(t, saveSeedProgress(path, progress))

	opts.LayerName = "two"
	_, err = loadSeedProgress(path, opts, &cfg, 4)
	require.ErrorContains(t, err, "does not match this job")
}

func Test_LoadSeedProgress_RejectsDifferentConfig(t *testing.T) {
	path := t.TempDir() + "/seed-progress.json"
	opts := SeedOptions{Zoom: []uint{1}, Bounds: pkg.Bounds{South: -1, North: 1, West: -1, East: 1}, LayerName: "resume"}
	cfg := seedTestResumeConfig()
	progress, err := loadSeedProgress(path, opts, &cfg, 1)
	require.NoError(t, err)
	require.NoError(t, saveSeedProgress(path, progress))

	cfg.Server.Port++
	_, err = loadSeedProgress(path, opts, &cfg, 1)
	require.ErrorContains(t, err, "does not match this job")
}

func Test_SeedPlan_UsesStableMultiZoomOrder(t *testing.T) {
	plan, err := newSeedPlan(SeedOptions{
		Zoom:      []uint{0, 1},
		Bounds:    pkg.Bounds{South: -90, North: 90, West: -180, East: 180},
		LayerName: "test",
	})
	require.NoError(t, err)
	require.Equal(t, uint64(5), plan.total)

	expected := []pkg.TileRequest{
		{LayerName: "test", Z: 0, X: 0, Y: 0},
		{LayerName: "test", Z: 1, X: 0, Y: 0},
		{LayerName: "test", Z: 1, X: 1, Y: 0},
		{LayerName: "test", Z: 1, X: 0, Y: 1},
		{LayerName: "test", Z: 1, X: 1, Y: 1},
	}
	for index, expectedRequest := range expected {
		actual, ok := plan.At(uint64(index))
		require.True(t, ok)
		require.Equal(t, expectedRequest, actual)
	}
}

func Test_SeedRunner_DoesNotAdvancePastCompletionGap(t *testing.T) {
	runner := seedRunner{
		progress:  seedProgress{Next: 5},
		completed: map[uint64]struct{}{6: {}},
	}
	runner.advanceProgress()
	require.Equal(t, uint64(5), runner.progress.Next)

	runner.completed[5] = struct{}{}
	runner.advanceProgress()
	require.Equal(t, uint64(7), runner.progress.Next)
}

func Test_LoadSeedProgress_RejectsIncompleteFile(t *testing.T) {
	path := t.TempDir() + "/seed-progress.json"
	require.NoError(t, os.WriteFile(path, []byte(`{"next":3}`), 0600))

	cfg := seedTestResumeConfig()
	opts := SeedOptions{Zoom: []uint{1}, Bounds: pkg.Bounds{South: -1, North: 1, West: -1, East: 1}, LayerName: "resume"}
	_, err := loadSeedProgress(path, opts, &cfg, 4)
	require.ErrorContains(t, err, "unsupported version")
}

func Test_LoadSeedProgress_DisabledDoesNotHashConfig(t *testing.T) {
	cfg := seedTestResumeConfig()
	cfg.Layers[0].Provider["not-json"] = func() {}
	opts := SeedOptions{Zoom: []uint{1}, LayerName: "resume"}

	progress, err := loadSeedProgress("", opts, &cfg, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(0), progress.Next)
}

func Test_DefaultSeedProgressFile_IsJobSpecific(t *testing.T) {
	cfg := seedTestResumeConfig()
	opts := SeedOptions{Zoom: []uint{1}, Bounds: pkg.Bounds{South: -1, North: 1, West: -1, East: 1}, LayerName: "resume"}

	first, err := DefaultSeedProgressFile(&cfg, opts)
	require.NoError(t, err)
	opts.Zoom = []uint{2}
	second, err := DefaultSeedProgressFile(&cfg, opts)
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	opts.Zoom = []uint{1}
	cfg.Server.Port++
	third, err := DefaultSeedProgressFile(&cfg, opts)
	require.NoError(t, err)
	require.NotEqual(t, first, third)
}

func Test_Seed_AllowsNilOutputForLibraryCompatibility(t *testing.T) {
	layer.RegisterProvider(seedTestResumeRegistration{})
	cfg := seedTestResumeConfig()

	require.NoError(t, Seed(&cfg, SeedOptions{
		Zoom:      []uint{0},
		Bounds:    pkg.Bounds{South: -1, North: 1, West: -1, East: 1},
		LayerName: "resume",
		NumThread: 1,
	}, nil))
}

func Test_DefaultSeedProgressFile_TracksReferencedEnvironment(t *testing.T) {
	cfg := seedTestResumeConfig()
	cfg.Layers[0].Provider["token"] = "env.SEED_TEST_TOKEN"
	opts := SeedOptions{Zoom: []uint{1}, LayerName: "resume"}

	t.Setenv("SEED_TEST_TOKEN", "one")
	first, err := DefaultSeedProgressFile(&cfg, opts)
	require.NoError(t, err)
	t.Setenv("SEED_TEST_TOKEN", "two")
	second, err := DefaultSeedProgressFile(&cfg, opts)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func Test_Seed_DefaultProgressFallsBackWhenLocationIsUnavailable(t *testing.T) {
	layer.RegisterProvider(seedTestResumeRegistration{})
	cfg := seedTestResumeConfig()
	blockingFile := t.TempDir() + "/file"
	require.NoError(t, os.WriteFile(blockingFile, []byte("not a directory"), 0600))

	var out bytes.Buffer
	err := SeedWithOptions(&cfg, SeedRunOptions{
		SeedOptions: SeedOptions{
			Zoom:      []uint{0},
			Bounds:    pkg.Bounds{South: -1, North: 1, West: -1, East: 1},
			LayerName: "resume",
			NumThread: 1,
		},
		MaxTiles:     DefaultSeedMaxTiles,
		ProgressFile: blockingFile + "/progress.json",
	}, &out)
	require.NoError(t, err)
	require.Contains(t, out.String(), "progress tracking disabled")
}

func Test_Seed_ExplicitProgressFailureIsReturned(t *testing.T) {
	layer.RegisterProvider(seedTestResumeRegistration{})
	cfg := seedTestResumeConfig()
	blockingFile := t.TempDir() + "/file"
	require.NoError(t, os.WriteFile(blockingFile, []byte("not a directory"), 0600))

	err := SeedWithOptions(&cfg, SeedRunOptions{
		SeedOptions: SeedOptions{
			Zoom:      []uint{0},
			Bounds:    pkg.Bounds{South: -1, North: 1, West: -1, East: 1},
			LayerName: "resume",
			NumThread: 1,
		},
		MaxTiles:         DefaultSeedMaxTiles,
		ProgressFile:     blockingFile + "/progress.json",
		ProgressRequired: true,
	}, io.Discard)
	require.Error(t, err)
}

func Test_SeedRunner_BoundsOutstandingOrdinalWindow(t *testing.T) {
	plan, err := newSeedPlan(SeedOptions{
		Zoom:      []uint{2},
		Bounds:    pkg.Bounds{South: -90, North: 90, West: -180, East: 180},
		LayerName: "test",
	})
	require.NoError(t, err)

	runner := seedRunner{
		plan:      plan,
		jobs:      make(chan seedJob, 2),
		window:    2,
		completed: map[uint64]struct{}{},
	}
	runner.dispatch()
	require.Equal(t, uint64(2), runner.nextDispatch)

	runner.inFlight = 0
	runner.dispatch()
	require.Equal(t, uint64(2), runner.nextDispatch)
}
