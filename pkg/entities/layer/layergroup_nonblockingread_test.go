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
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// delayedCache waits either for `delay` to elapse or ctx to be cancelled, whichever comes first,
// then reports lookupImg/lookupErr. It records how many times Lookup was called and how many
// completed (as opposed to being abandoned via ctx cancellation) so tests can assert both that the
// faster side won and that the loser wasn't left blocking forever.
type delayedCache struct {
	delay     time.Duration
	lookupImg *pkg.Image
	lookupErr error
	lookups   atomic.Int32
	completed atomic.Int32
	saveCalls atomic.Int32
	lastSaved atomic.Pointer[pkg.Image]
}

func (c *delayedCache) Lookup(ctx context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	c.lookups.Add(1)
	select {
	case <-time.After(c.delay):
		c.completed.Add(1)
		return c.lookupImg, c.lookupErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *delayedCache) Save(_ context.Context, _ pkg.TileRequest, img *pkg.Image) error {
	c.saveCalls.Add(1)
	c.lastSaved.Store(img)
	return nil
}

// delayedProvider mirrors delayedCache but on the generation side, also respecting ctx
// cancellation instead of always sleeping out the full delay.
type delayedProvider struct {
	delay         time.Duration
	genImg        *pkg.Image
	genErr        error
	generateCalls atomic.Int32
}

func (p *delayedProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *delayedProvider) GenerateTile(ctx context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	select {
	case <-time.After(p.delay):
		return p.genImg, p.genErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *delayedProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

func newRacingLayerGroup(l *Layer, c cache.Cache) *LayerGroup {
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	return &LayerGroup{
		layers:            []*Layer{l},
		DefaultCache:      c,
		cacheHitCounter:   noop.Int64Counter{},
		cacheMissCounter:  noop.Int64Counter{},
		cacheWriteLimiter: make(chan struct{}, maxConcurrentCacheWrites),
	}
}

// A fast cache hit should win the race against slower generation, and generation should not be
// left running past what's needed - most importantly the response shouldn't wait on it.
func Test_RenderTile_NonBlockingRead_FastCacheHitWins(t *testing.T) {
	provider := &delayedProvider{delay: 200 * time.Millisecond, genImg: &pkg.Image{Content: []byte("generated")}}
	c := &delayedCache{delay: 5 * time.Millisecond, lookupImg: &pkg.Image{Content: []byte("cached")}}
	nonBlocking := true

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config:   config.LayerConfig{NonBlockingRead: &nonBlocking},
	}
	lg := newRacingLayerGroup(l, c)

	start := time.Now()
	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, "cached", string(img.Content))
	require.Less(t, elapsed, 150*time.Millisecond, "should return once the fast cache hit lands, not wait for slow generation")
}

// When generation is faster than the cache, its result should be used and it should still be
// written back to cache same as the blocking path does.
func Test_RenderTile_NonBlockingRead_FastGenerationWinsAndStillWritesCache(t *testing.T) {
	provider := &delayedProvider{delay: 5 * time.Millisecond, genImg: &pkg.Image{Content: []byte("generated")}}
	c := &delayedCache{delay: 200 * time.Millisecond}
	nonBlocking := true

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config:   config.LayerConfig{NonBlockingRead: &nonBlocking},
	}
	lg := newRacingLayerGroup(l, c)

	start := time.Now()
	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, "generated", string(img.Content))
	require.Less(t, elapsed, 150*time.Millisecond, "should return once generation wins, not wait for the slow cache")

	require.Eventually(t, func() bool {
		return c.saveCalls.Load() == 1
	}, time.Second, 5*time.Millisecond, "generated tile should still be written back to cache")
}

// Blocking mode (the default, no NonBlockingRead set anywhere) must behave exactly as before:
// generation never starts until the cache lookup completes.
func Test_RenderTile_BlockingModeUnchangedByDefault(t *testing.T) {
	provider := &delayedProvider{delay: 0, genImg: &pkg.Image{Content: []byte("generated")}}
	c := &delayedCache{delay: 30 * time.Millisecond}

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
	}
	lg := newRacingLayerGroup(l, c)

	start := time.Now()
	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, "generated", string(img.Content))
	require.GreaterOrEqual(t, elapsed, 30*time.Millisecond, "blocking mode must wait for the cache lookup before generating")
	require.Equal(t, int32(1), provider.generateCalls.Load())
}

// The cache's own configured default applies when the layer doesn't set its own override.
func Test_RenderTile_NonBlockingRead_InheritsCacheDefaultWhenLayerUnset(t *testing.T) {
	provider := &delayedProvider{delay: 200 * time.Millisecond, genImg: &pkg.Image{Content: []byte("generated")}}
	inner := &delayedCache{delay: 5 * time.Millisecond, lookupImg: &pkg.Image{Content: []byte("cached")}}
	wrapped := cache.CacheWrapper{Name: "test", Cache: inner, NonBlockingRead: true}

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    wrapped,
		// Config.NonBlockingRead intentionally left nil to inherit the cache's default.
	}
	lg := newRacingLayerGroup(l, wrapped)

	start := time.Now()
	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, "cached", string(img.Content))
	require.Less(t, elapsed, 150*time.Millisecond)
}

// A per-layer override of false must win even when the cache's own default is non-blocking.
func Test_RenderTile_NonBlockingRead_LayerOverrideDisablesCacheDefault(t *testing.T) {
	provider := &delayedProvider{delay: 0, genImg: &pkg.Image{Content: []byte("generated")}}
	inner := &delayedCache{delay: 30 * time.Millisecond}
	wrapped := cache.CacheWrapper{Name: "test", Cache: inner, NonBlockingRead: true}
	layerOverride := false

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    wrapped,
		Config:   config.LayerConfig{NonBlockingRead: &layerOverride},
	}
	lg := newRacingLayerGroup(l, wrapped)

	start := time.Now()
	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, "generated", string(img.Content))
	require.GreaterOrEqual(t, elapsed, 30*time.Millisecond, "layer override to false must force blocking behavior despite the cache default")
}

// A per-layer override of true must win even when the cache's own default is blocking.
func Test_RenderTile_NonBlockingRead_LayerOverrideEnablesOverCacheDefault(t *testing.T) {
	provider := &delayedProvider{delay: 200 * time.Millisecond, genImg: &pkg.Image{Content: []byte("generated")}}
	inner := &delayedCache{delay: 5 * time.Millisecond, lookupImg: &pkg.Image{Content: []byte("cached")}}
	wrapped := cache.CacheWrapper{Name: "test", Cache: inner, NonBlockingRead: false}
	layerOverride := true

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    wrapped,
		Config:   config.LayerConfig{NonBlockingRead: &layerOverride},
	}
	lg := newRacingLayerGroup(l, wrapped)

	start := time.Now()
	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, "cached", string(img.Content))
	require.Less(t, elapsed, 150*time.Millisecond)
}

// If generation errors while the cache lookup is still outstanding, the cache result should still
// be waited on rather than failing immediately - a hit could still save the request.
func Test_RenderTile_NonBlockingRead_GenerationErrorFallsBackToCacheHit(t *testing.T) {
	provider := &delayedProvider{delay: 5 * time.Millisecond, genErr: errors.New("boom")}
	c := &delayedCache{delay: 30 * time.Millisecond, lookupImg: &pkg.Image{Content: []byte("cached")}}
	nonBlocking := true

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config:   config.LayerConfig{NonBlockingRead: &nonBlocking},
	}
	lg := newRacingLayerGroup(l, c)

	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, "cached", string(img.Content))
}

// If generation errors and the cache lookup also misses, the generation error should surface.
func Test_RenderTile_NonBlockingRead_GenerationErrorSurfacesOnCacheMissToo(t *testing.T) {
	provider := &delayedProvider{delay: 5 * time.Millisecond, genErr: errors.New("boom")}
	c := &delayedCache{delay: 30 * time.Millisecond}
	nonBlocking := true

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config:   config.LayerConfig{NonBlockingRead: &nonBlocking},
	}
	lg := newRacingLayerGroup(l, c)

	img, err := lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})

	require.Error(t, err)
	require.Nil(t, img)
	require.Contains(t, err.Error(), "boom")
}

// Cancelling the caller's context must stop both goroutines from hanging the request, regardless
// of whichever would have "won" the race.
func Test_RenderTile_NonBlockingRead_RespectsContextCancellation(t *testing.T) {
	provider := &delayedProvider{delay: time.Hour, genImg: &pkg.Image{Content: []byte("generated")}}
	c := &delayedCache{delay: time.Hour, lookupImg: &pkg.Image{Content: []byte("cached")}}
	nonBlocking := true

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config:   config.LayerConfig{NonBlockingRead: &nonBlocking},
	}
	lg := newRacingLayerGroup(l, c)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	var img *pkg.Image
	var err error
	go func() {
		img, err = lg.RenderTile(ctx, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RenderTile did not respect context cancellation and is still blocked")
	}

	require.Nil(t, img)
	require.Error(t, err)
}
