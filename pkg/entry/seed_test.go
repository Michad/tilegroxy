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
	"sync"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
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

func (seedTestPanicRegistration) Name() string          { return "seed-test-panic-provider" }
func (seedTestPanicRegistration) InitializeConfig() any { return struct{}{} }
func (seedTestPanicRegistration) Initialize(_ any, _ config.ClientConfig, _ config.ErrorMessages, _ *layer.LayerGroup, _ *datastore.DatastoreRegistry) (layer.Provider, error) {
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

	require.NotPanics(t, func() {
		seedThread(&wg, SeedOptions{Verbose: true}, &out, lg, 0, []pkg.TileRequest{{LayerName: "panics", Z: 1, X: 0, Y: 0}}, errs)
	})

	wg.Wait()
	close(errs)
	require.Contains(t, out.String(), "panicked")

	// The thread abandoned the rest of its chunk, so recovering must not swallow the panic.
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
