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

package layer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

type slowGenerateProvider struct {
	generateCalls atomic.Int32
	delay         time.Duration
}

func (p *slowGenerateProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *slowGenerateProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	time.Sleep(p.delay)
	return &pkg.Image{Content: []byte("tile")}, nil
}

func (p *slowGenerateProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// alwaysMissCache always reports a miss and records how many times Save is called.
type alwaysMissCache struct {
	saveCalls atomic.Int32
}

func (c *alwaysMissCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c *alwaysMissCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	c.saveCalls.Add(1)
	return nil
}

// RenderTile is exported API, so a library consumer can call it with a plain stdlib context
// rather than one from pkg.NewRequestContext. A context carrying no restriction info has to be
// treated as unrestricted instead of dereferencing the nil pointers that come back.
func Test_LayerGroup_RenderTile_PlainContextBackgroundDoesNotPanic(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	c := &alwaysMissCache{}

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	lg := &LayerGroup{
		layers:           []*Layer{l},
		DefaultCache:     c,
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}

	var img *pkg.Image
	var err error
	require.NotPanics(t, func() {
		img, err = lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	})
	require.NoError(t, err)
	require.NotNil(t, img)
}

// blockingCache never returns from Save until the test releases it, so background cache writes
// pile up and the limiter's bound becomes observable.
type blockingCache struct {
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	unblock  chan struct{}
}

func (c *blockingCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c *blockingCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	n := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	for {
		old := c.maxSeen.Load()
		if n <= old || c.maxSeen.CompareAndSwap(old, n) {
			break
		}
	}
	<-c.unblock
	return nil
}

// Without a bound, a slow cache backend plus sustained misses accumulates goroutines and the
// images they pin. The tiles are distinct so singleflight doesn't collapse the misses.
func Test_LayerGroup_RenderTile_BoundsConcurrentCacheWrites(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	c := &blockingCache{unblock: make(chan struct{})}
	defer close(c.unblock)

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	lg := &LayerGroup{
		layers:            []*Layer{l},
		DefaultCache:      c,
		cacheHitCounter:   noop.Int64Counter{},
		cacheMissCounter:  noop.Int64Counter{},
		cacheWriteLimiter: make(chan struct{}, maxConcurrentCacheWrites),
	}

	const n = maxConcurrentCacheWrites * 3
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			_, _ = lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 20, X: i, Y: 0})
		}(i)
	}
	wg.Wait()

	require.LessOrEqual(t, int(c.maxSeen.Load()), maxConcurrentCacheWrites)
}

// alwaysHitCache returns a canned tile on every lookup, regardless of the request.
type alwaysHitCache struct{}

func (alwaysHitCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{Content: []byte("cached")}, nil
}

func (alwaysHitCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return nil
}

// A zoom limit added after a tile was cached (or a tile seeded outside the layer's configured
// range) must still be enforced on a cache hit, not just on the miss path that reaches the
// provider.
func Test_LayerGroup_RenderTile_RejectsOutOfZoomRangeEvenOnCacheHit(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	c := alwaysHitCache{}
	minZoom := 4

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config:   config.LayerConfig{MinZoom: &minZoom},
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	lg := &LayerGroup{
		layers:           []*Layer{l},
		DefaultCache:     c,
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}

	_, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})

	require.Error(t, err)
	var rangeErr pkg.RangeError
	require.ErrorAs(t, err, &rangeErr)
	require.Equal(t, int32(0), provider.generateCalls.Load())
}

type panicOnSaveCache struct{}

func (panicOnSaveCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (panicOnSaveCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	panic("simulated panic from a buggy Cache.Save implementation")
}

// writeCache runs on its own goroutine after a cache miss, so an unrecovered panic from a
// third-party cache would take down the process over a write no client is waiting on.
func Test_WriteCache_RecoversFromPanic(t *testing.T) {
	require.NotPanics(t, func() {
		writeCache(context.Background(), panicOnSaveCache{}, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0}, &pkg.Image{Content: []byte("x")})
	})
}

type errorSaveCache struct{}

func (errorSaveCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (errorSaveCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return errors.New("simulated cache save failure")
}

func Test_LayerGroup_RenderTileSync_ReportsCacheSaveFailure(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	cache := errorSaveCache{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    cache,
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	lg := &LayerGroup{
		layers:           []*Layer{l},
		DefaultCache:     cache,
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}

	_, err := lg.RenderTileSync(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.ErrorContains(t, err, "simulated cache save failure")
}
