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

package layer

import (
	"context"
	"strconv"
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

// Reproduces the cache-stampede scenario: many concurrent requests for the same tile on a
// cache miss should coalesce into a single upstream render via singleflight, not N renders.
func Test_LayerGroup_RenderTile_CoalescesConcurrentMisses(t *testing.T) {
	provider := &slowGenerateProvider{delay: 50 * time.Millisecond}
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

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	// Collect results rather than asserting inside the goroutines: require.* calls
	// runtime.Goexit, which would skip the wg.Done and hang the test instead of failing it.
	type result struct {
		img *pkg.Image
		err error
	}
	results := make(chan result, n)
	for range n {
		go func() {
			defer wg.Done()
			ctx := pkg.BackgroundContext()
			img, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
			results <- result{img, err}
		}()
	}
	wg.Wait()
	close(results)

	for r := range results {
		require.NoError(t, r.err)
		require.NotNil(t, r.img)
	}

	require.Equal(t, int32(1), provider.generateCalls.Load(), "expected concurrent misses for the same tile to coalesce into a single upstream render")
}

// RenderTile is exported API a library consumer could call with a plain context.Background()
// (a stdlib context, not one built via pkg.NewRequestContext/pkg.BackgroundContext). It used to
// dereference nil pointers from LimitLayersFromContext et al and panic; it should instead treat
// a context with no restriction info set as unrestricted, the same default NewRequestContext
// itself installs.
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

// ctxEchoProvider renders a tile whose content is read from a context value, standing in for a
// provider whose URL contains a {ctx.<header>} placeholder (which internal/providers resolves
// against the rendering context at render time).
type ctxEchoProvider struct {
	key           string
	generateCalls atomic.Int32
	delay         time.Duration
}

func (p *ctxEchoProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *ctxEchoProvider) GenerateTile(ctx context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	time.Sleep(p.delay)
	val, _ := ctx.Value(p.key).(string)
	return &pkg.Image{Content: []byte(val)}, nil
}

// The central risk of coalescing: singleflight hands the leader's rendered tile to every waiter.
// For a layer whose provider resolves {ctx.*} placeholders, every HTTP header of the request is in
// the rendering context, so a waiter would receive a tile fetched with the *leader's* credentials.
// Concurrent callers with different per-user context values must each get their own scoped result.
func Test_LayerGroup_RenderTile_DoesNotCoalesceRequestScopedLayers(t *testing.T) {
	const header = "Authorization"
	provider := &ctxEchoProvider{key: header, delay: 25 * time.Millisecond}
	c := &alwaysMissCache{}

	l := &Layer{
		ID:       "secret",
		Pattern:  []layerSegment{{value: "secret", placeholder: false}},
		Provider: provider,
		Cache:    c,
		Config: config.LayerConfig{
			ID:       "secret",
			Provider: map[string]any{"name": "url", "url": "https://example.com/{ctx." + header + "}/{z}/{x}/{y}"},
		},
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
		noCoalesceLayers: requestScopedLayers([]config.LayerConfig{l.Config}),
	}

	require.True(t, lg.noCoalesceLayers["secret"], "a layer whose provider config uses {ctx.} must be marked request-scoped")

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	// Collect per-caller results rather than asserting inside the goroutines: require.* calls
	// runtime.Goexit, which would skip the wg.Done and hang the test instead of failing it.
	type seen struct {
		want string
		got  string
		err  error
	}
	results := make(chan seen, n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			token := "user-token-" + strconv.Itoa(i)
			//nolint:staticcheck // matching how NewRequestContext stores header values
			ctx := context.WithValue(pkg.BackgroundContext(), header, token)
			img, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "secret", Z: 1, X: 0, Y: 0})
			s := seen{want: token, err: err}
			if img != nil {
				s.got = string(img.Content)
			}
			results <- s
		}(i)
	}
	wg.Wait()
	close(results)

	for s := range results {
		require.NoError(t, s.err)
		// Each caller must see the tile rendered under its OWN identity, never a
		// concurrent caller's.
		require.Equal(t, s.want, s.got)
	}
}

// {layer.*} placeholders resolve from the layer pattern matches, which derive from the requested
// layer name - and the layer name is part of the singleflight key. So two callers who coalesce
// necessarily requested the same layer name and get the same matches; such layers stay safe to
// coalesce and must not be marked request-scoped.
func Test_RequestScopedLayers_Classification(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "plain", Provider: map[string]any{"name": "url", "url": "https://example.com/{z}/{x}/{y}"}},
		{ID: "env", Provider: map[string]any{"name": "url", "url": "https://example.com/{env.KEY}/{z}"}},
		{ID: "layerph", Pattern: "layerph_{p}", Provider: map[string]any{"name": "url", "url": "https://example.com/{layer.p}/{z}"}},
		{ID: "ctxph", Provider: map[string]any{"name": "url", "url": "https://example.com/{ctx.Authorization}/{z}"}},
		{ID: "nested", Provider: map[string]any{
			"name": "fallback",
			"primary": map[string]any{
				"name": "url", "url": "https://example.com/{ctx.X-Api-Key}/{z}",
			},
		}},
		// refs a request-scoped layer; the ref provider rebuilds the child context from the
		// original request so headers propagate across the edge.
		{ID: "refsCtx", Provider: map[string]any{"name": "ref", "layer": "ctxph"}},
		{ID: "refsPlain", Provider: map[string]any{"name": "ref", "layer": "plain"}},
		// transitively unsafe: refsCtx is unsafe.
		{ID: "refsRefsCtx", Provider: map[string]any{"name": "ref", "layer": "refsCtx"}},
	}

	got := requestScopedLayers(layers)

	require.False(t, got["plain"])
	require.False(t, got["env"])
	require.False(t, got["layerph"], "{layer.} matches derive from the layer name, which is already in the singleflight key")
	require.False(t, got["refsPlain"])

	require.True(t, got["ctxph"])
	require.True(t, got["nested"], "{ctx.} nested inside another provider's config must still be found")
	require.True(t, got["refsCtx"], "a ref to a request-scoped layer is itself request-scoped")
	require.True(t, got["refsRefsCtx"], "request-scoped-ness must propagate transitively across refs")
}

// Every waiter on a singleflight key used to receive the identical *pkg.Image pointer, and callers
// do mutate the result in place (internal/providers/fallback.go sets ForceSkipCache on an image
// that may have come back through Ref.GenerateTile -> RenderTile). One caller's mutation must not
// be visible to another.
func Test_LayerGroup_RenderTile_CoalescedWaitersGetIndependentImages(t *testing.T) {
	provider := &slowGenerateProvider{delay: 25 * time.Millisecond}
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

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	imgs := make([]*pkg.Image, n)
	errs := make([]error, n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			img, err := lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "test", Z: 5, X: 1, Y: 1})
			errs[i] = err
			// Half the callers mutate in place, the way fallback.go does. Under -race this
			// also catches the waiters sharing one struct.
			if img != nil && i%2 == 0 {
				img.ForceSkipCache = true
				img.ContentType = "mutated/" + strconv.Itoa(i)
			}
			imgs[i] = img
		}(i)
	}
	wg.Wait()

	// Asserted here rather than in the goroutines: require.* calls runtime.Goexit, which would
	// skip the wg.Done and hang the test instead of failing it.
	for i := range n {
		require.NoError(t, errs[i])
		require.NotNil(t, imgs[i])
	}

	require.Equal(t, int32(1), provider.generateCalls.Load(), "expected the renders to coalesce")

	for i := range n {
		if i%2 == 1 {
			require.False(t, imgs[i].ForceSkipCache, "a waiter's image was mutated by another waiter")
			require.Empty(t, imgs[i].ContentType, "a waiter's image was mutated by another waiter")
		}
	}
}

// nilImageProvider reports success but returns no image. composite_mvt.go and blend.go both
// already defend against a nested provider doing this, so it's reachable rather than theoretical.
type nilImageProvider struct{}

func (nilImageProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (nilImageProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

// A provider returning (nil, nil) used to reach an unchecked type assertion and then dereference
// the nil image at `if !img.ForceSkipCache`, panicking on the request goroutine. It should surface
// as an error instead.
func Test_LayerGroup_RenderTile_NilImageIsAnErrorNotAPanic(t *testing.T) {
	c := &alwaysMissCache{}

	l := &Layer{
		ID:       "nil",
		Pattern:  []layerSegment{{value: "nil", placeholder: false}},
		Provider: nilImageProvider{},
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
		img, err = lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "nil", Z: 1, X: 0, Y: 0})
	})
	require.Error(t, err)
	require.Nil(t, img)
}

// Same for a layer that skips coalescing - the nil check has to cover both paths.
func Test_LayerGroup_RenderTile_NilImageIsAnErrorWhenNotCoalescing(t *testing.T) {
	c := &alwaysMissCache{}

	cfg := config.LayerConfig{
		ID:       "nil",
		Provider: map[string]any{"name": "url", "url": "https://example.com/{ctx.Authorization}"},
	}

	l := &Layer{
		ID:       "nil",
		Pattern:  []layerSegment{{value: "nil", placeholder: false}},
		Provider: nilImageProvider{},
		Cache:    c,
		Config:   cfg,
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
		noCoalesceLayers: requestScopedLayers([]config.LayerConfig{cfg}),
	}

	var img *pkg.Image
	var err error
	require.NotPanics(t, func() {
		img, err = lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "nil", Z: 1, X: 0, Y: 0})
	})
	require.Error(t, err)
	require.Nil(t, img)
}

// ctxAwareProvider mimics a real provider doing a cancellable HTTP call: it fails if the context
// it renders under is cancelled before its work finishes.
type ctxAwareProvider struct {
	generateCalls atomic.Int32
	delay         time.Duration
}

func (p *ctxAwareProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *ctxAwareProvider) GenerateTile(ctx context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	select {
	case <-time.After(p.delay):
		return &pkg.Image{Content: []byte("tile")}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// The leader's cancellation used to be the only cancellation source for a coalesced render, so a
// leader disconnecting failed every follower with its context.Canceled even though their own
// requests were alive. The render now happens under a context detached from any single caller.
func Test_LayerGroup_RenderTile_LeaderCancellationDoesNotFailFollowers(t *testing.T) {
	provider := &ctxAwareProvider{delay: 60 * time.Millisecond}
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

	req := pkg.TileRequest{LayerName: "test", Z: 9, X: 3, Y: 4}

	leaderCtx, cancelLeader := context.WithCancel(pkg.BackgroundContext())
	var leaderStarted sync.WaitGroup
	leaderStarted.Add(1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		leaderStarted.Done()
		_, _ = lg.RenderTile(leaderCtx, req)
	}()

	leaderStarted.Wait()
	time.Sleep(10 * time.Millisecond)

	// Follower joins the in-flight render, then the leader goes away.
	wg.Add(1)
	var followerErr error
	var followerImg *pkg.Image
	go func() {
		defer wg.Done()
		followerImg, followerErr = lg.RenderTile(pkg.BackgroundContext(), req)
	}()

	time.Sleep(10 * time.Millisecond)
	cancelLeader()

	wg.Wait()

	require.NoError(t, followerErr, "follower must not inherit the leader's cancellation")
	require.NotNil(t, followerImg)
}

// blockingCache never returns from Save until the test releases it, letting us pile up
// concurrent background cache writes to prove the limiter actually bounds them.
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

// Without a bound, a slow cache backend plus sustained misses accumulates unbounded goroutines
// and the *pkg.Image buffers they pin. Fires many concurrent misses for distinct tiles (so
// singleflight coalescing doesn't collapse them) against a cache whose Save blocks, and confirms
// the number of concurrently in-flight background writes never exceeds maxConcurrentCacheWrites.
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

type panicOnSaveCache struct{}

func (panicOnSaveCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (panicOnSaveCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	panic("simulated panic from a buggy Cache.Save implementation")
}

// writeCache runs detached on its own goroutine after a cache miss (see the `go writeCache(...)`
// call site in RenderTile), so an unrecovered panic there - e.g. from a buggy third-party cache -
// used to take down the entire process for a background write the client isn't even waiting on.
func Test_WriteCache_RecoversFromPanic(t *testing.T) {
	require.NotPanics(t, func() {
		writeCache(context.Background(), panicOnSaveCache{}, pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0}, &pkg.Image{Content: []byte("x")})
	})
}
