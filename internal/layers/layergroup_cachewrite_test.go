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

package layers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

type slowGenerateProvider struct {
	generateCalls atomic.Int32
	delay         time.Duration
}

func (p *slowGenerateProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *slowGenerateProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	time.Sleep(p.delay)
	return &pkg.Image{Content: []byte("tile")}, nil
}

func (p *slowGenerateProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// Records how many times Lookup and Save are called
type alwaysMissCache struct {
	lookupCalls atomic.Int32
	saveCalls   atomic.Int32
}

func (c *alwaysMissCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	c.lookupCalls.Add(1)
	return nil, nil
}

func (c *alwaysMissCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	c.saveCalls.Add(1)
	return nil
}

func (c *alwaysMissCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

// Exported, so library callers may pass a plain stdlib context. Missing restriction info must mean unrestricted, not a nil dereference
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

// Never returns from Save until released, so the limiter's bound becomes observable
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

func (c *blockingCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

// Without a bound, a slow cache plus sustained misses piles up goroutines. Distinct tiles avoid singleflight collapsing them
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

type alwaysHitCache struct{}

func (alwaysHitCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{Content: []byte("cached")}, nil
}

func (alwaysHitCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return nil
}

func (alwaysHitCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

// A zoom limit added after caching, or a tile seeded outside the range, must be enforced on a hit too
func Test_LayerGroup_RenderTile_RejectsOutOfZoomRangeEvenOnCacheHit(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	c := alwaysHitCache{}
	minZoom := 4

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		metadata: ResolvedMetadata{Limits: Limits{MinZoom: &minZoom}},
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	lg := &LayerGroup{
		layers:           []*Layer{l},
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}

	_, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})

	require.Error(t, err)
	var rangeErr pkg.RangeError
	require.ErrorAs(t, err, &rangeErr)
	require.Equal(t, int32(0), provider.generateCalls.Load())
}

// Lets analytics report `cached: true`
func Test_LayerGroup_RenderTile_CacheHitSetsContextFlag(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	c := alwaysHitCache{}

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
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/test/1/0/0", nil)
	ctx := pkg.NewRequestContext(req)

	img, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)
	require.NotNil(t, img)

	cached, ok := pkg.CachedFromContext(ctx)
	require.True(t, ok)
	require.True(t, *cached)
}

// Both the direct and coalesced paths must leave the flag false so analytics reports `cached: false`
func Test_LayerGroup_RenderTile_CacheMissLeavesContextFlagFalse(t *testing.T) {
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
		layers:            []*Layer{l},
		cacheHitCounter:   noop.Int64Counter{},
		cacheMissCounter:  noop.Int64Counter{},
		cacheWriteLimiter: make(chan struct{}, maxConcurrentCacheWrites),
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/test/1/0/0", nil)
	ctx := pkg.NewRequestContext(req)

	img, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)
	require.NotNil(t, img)

	cached, ok := pkg.CachedFromContext(ctx)
	require.True(t, ok)
	require.False(t, *cached)
}

// A skipCache layer never touches the cache, so it always reports a miss
func Test_LayerGroup_RenderTile_SkipCacheLeavesContextFlagFalse(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	c := alwaysHitCache{}

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config:   config.LayerConfig{SkipCache: true},
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	lg := &LayerGroup{
		layers:           []*Layer{l},
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/test/1/0/0", nil)
	ctx := pkg.NewRequestContext(req)

	img, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)
	require.NotNil(t, img)

	cached, ok := pkg.CachedFromContext(ctx)
	require.True(t, ok)
	require.False(t, *cached)
}

type panicOnSaveCache struct{}

func (panicOnSaveCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (panicOnSaveCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	panic("simulated panic from a buggy Cache.Save implementation")
}

func (panicOnSaveCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

// On its own goroutine, an unrecovered panic from a third-party cache would crash the process
func Test_WriteCache_RecoversFromPanic(t *testing.T) {
	require.NotPanics(t, func() {
		writeCache(context.Background(), panicOnSaveCache{}, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0}, &pkg.Image{Content: []byte("x")})
	})
}

// Captures the tenant ID Save sees on its context
type recordingIdentityCache struct {
	saved   chan string
	lookups chan string
}

func (c *recordingIdentityCache) Lookup(ctx context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	c.lookups <- tenantOf(ctx)
	return nil, nil
}

func (c *recordingIdentityCache) Save(ctx context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	c.saved <- tenantOf(ctx)
	return nil
}

func (c *recordingIdentityCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

func tenantOf(ctx context.Context) string {
	if t, ok := pkg.TenantIDFromContext(ctx); ok && t != nil {
		return *t
	}

	return ""
}

// The write runs on a fresh context, so a tenant-keyed cache could save under a different key than the lookup
func Test_LayerGroup_RenderTile_CacheWriteSeesTenant(t *testing.T) {
	provider := &slowGenerateProvider{delay: 0}
	c := &recordingIdentityCache{saved: make(chan string, 1), lookups: make(chan string, 1)}

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
		cacheHitCounter:   noop.Int64Counter{},
		cacheMissCounter:  noop.Int64Counter{},
		cacheWriteLimiter: make(chan struct{}, 1),
	}

	ctx := pkg.BackgroundContext()
	pkg.SetIdentity(ctx, "some-user", "tenant_a")

	_, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	require.Equal(t, "tenant_a", <-c.lookups)

	select {
	case saved := <-c.saved:
		require.Equal(t, "tenant_a", saved)
	case <-time.After(5 * time.Second):
		t.Fatal("cache write never happened")
	}
}

// Finishes its write only after a delay, so a Close that didn't wait would return early
type slowSaveCache struct {
	saved atomic.Int32
	delay time.Duration
}

func (c *slowSaveCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c *slowSaveCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	time.Sleep(c.delay)
	c.saved.Add(1)
	return nil
}

func (c *slowSaveCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

// Otherwise a seed would report tiles it never cached and close the cache under the write
func Test_LayerGroup_WaitForCacheWrites_WaitsForInFlightWrites(t *testing.T) {
	c := &slowSaveCache{delay: 50 * time.Millisecond}
	lg := newCacheWriteTestGroup(c)

	_, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	require.NoError(t, lg.WaitForCacheWrites(context.Background()))
	require.Equal(t, int32(1), c.saved.Load())
}

// Waiting forever on a wedged cache would hang shutdown, so the caller's deadline wins
func Test_LayerGroup_WaitForCacheWrites_GivesUpWhenContextEnds(t *testing.T) {
	c := &blockingCache{unblock: make(chan struct{})}
	defer close(c.unblock)

	lg := newCacheWriteTestGroup(c)

	_, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, lg.WaitForCacheWrites(ctx), context.DeadlineExceeded)
}

func newCacheWriteTestGroup(c cache.Cache) *LayerGroup {
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: &slowGenerateProvider{delay: 0},
		Cache:    c,
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	return &LayerGroup{
		layers:            []*Layer{l},
		cacheHitCounter:   noop.Int64Counter{},
		cacheMissCounter:  noop.Int64Counter{},
		cacheWriteLimiter: make(chan struct{}, maxConcurrentCacheWrites),
	}
}

// Returns what a multi tier cache used to: a usable tile plus a degraded tier's error
type hitWithErrorCache struct {
	img *pkg.Image
}

func (c hitWithErrorCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return c.img, errors.New("tier unavailable")
}

func (hitWithErrorCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return nil
}

func (hitWithErrorCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

// A tile in hand shouldn't become an error response because the cache also reported a problem
func Test_LayerGroup_RenderTile_CacheHitWithErrorStillServesTile(t *testing.T) {
	img := pkg.Image{Content: []byte("cached")}

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: &slowGenerateProvider{delay: 0},
		Cache:    hitWithErrorCache{img: &img},
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	lg := &LayerGroup{
		layers:            []*Layer{l},
		cacheHitCounter:   noop.Int64Counter{},
		cacheMissCounter:  noop.Int64Counter{},
		cacheWriteLimiter: make(chan struct{}, 1),
	}

	out, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)
	require.Equal(t, &img, out)
}
