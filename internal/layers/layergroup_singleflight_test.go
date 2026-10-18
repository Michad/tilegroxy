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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

func newSingleflightTestLayerGroup(l *Layer, c *alwaysMissCache) *LayerGroup {
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}
	l.allowCoalesce = true
	l.Cache = c

	return &LayerGroup{
		layers:           []*Layer{l},
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}
}

// Concurrent identical requests must collapse into one provider call with every caller getting the result
func Test_LayerGroup_RenderTile_CoalescesConcurrentIdenticalRequests(t *testing.T) {
	provider := &slowGenerateProvider{delay: 50 * time.Millisecond}
	c := &alwaysMissCache{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
	}
	lg := newSingleflightTestLayerGroup(l, c)
	lg.cacheWriteLimiter = make(chan struct{}, maxConcurrentCacheWrites)

	const n = 25
	var wg sync.WaitGroup
	wg.Add(n)
	imgs := make([]*pkg.Image, n)
	errs := make([]error, n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			imgs[i], errs[i] = lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 5, X: 3, Y: 3})
		}(i)
	}
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		require.NotNil(t, imgs[i])
		require.Equal(t, "tile", string(imgs[i].Content))
	}
	require.Equal(t, int32(1), provider.generateCalls.Load(), "concurrent requests for the same tile should only invoke the provider once")
}

// Only identical keys should share an in-flight call
func Test_LayerGroup_RenderTile_DoesNotCoalesceDifferentKeys(t *testing.T) {
	provider := &slowGenerateProvider{delay: 100 * time.Millisecond}
	c := &alwaysMissCache{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
	}
	lg := newSingleflightTestLayerGroup(l, c)
	lg.cacheWriteLimiter = make(chan struct{}, maxConcurrentCacheWrites)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)

	start := time.Now()
	for i := range n {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = lg.RenderTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 5, X: i, Y: 0})
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i := range n {
		require.NoError(t, errs[i])
	}
	require.Equal(t, int32(n), provider.generateCalls.Load(), "distinct tile keys must each invoke the provider independently")
	// Serialized keys would take roughly n*delay. Generous headroom tolerates sandbox scheduling jitter
	require.Less(t, elapsed, 5*time.Duration(n/2)*provider.delay, "requests for distinct tiles appear to be serialized rather than running concurrently")
}

// Keeps a leader in flight deterministically while a waiter's context expires
type blockingUntilReleasedProvider struct {
	generateCalls atomic.Int32
	started       chan struct{}
	release       chan struct{}
}

func (p *blockingUntilReleasedProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *blockingUntilReleasedProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	close(p.started)
	<-p.release
	return &pkg.Image{Content: []byte("tile")}, nil
}

func (p *blockingUntilReleasedProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// A waiter's deadline must expire independently, without waiting for or cancelling the leader or other waiters
func Test_LayerGroup_RenderTile_WaiterContextExpiresIndependentlyOfLeader(t *testing.T) {
	provider := &blockingUntilReleasedProvider{started: make(chan struct{}), release: make(chan struct{})}
	c := &alwaysMissCache{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
	}
	lg := newSingleflightTestLayerGroup(l, c)
	lg.cacheWriteLimiter = make(chan struct{}, maxConcurrentCacheWrites)

	tileRequest := pkg.TileRequest{LayerName: "test", Z: 5, X: 3, Y: 3}

	// Leader starts the fetch and blocks until released
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		_, _ = lg.RenderTile(context.Background(), tileRequest)
	}()

	<-provider.started // leader is now inside GenerateTile, blocked on release

	// Waiter joins the same key with a deadline that expires long before release
	waiterCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	waiterStart := time.Now()
	_, err := lg.RenderTile(waiterCtx, tileRequest)
	waiterElapsed := time.Since(waiterStart)

	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, waiterElapsed, 500*time.Millisecond, "waiter should return promptly on its own deadline rather than waiting for the leader")

	select {
	case <-leaderDone:
		t.Fatal("leader should not have completed yet - the waiter's timeout must not cancel the leader")
	default:
	}

	// Let the leader finish so no goroutines leak
	close(provider.release)
	<-leaderDone
	require.Equal(t, int32(1), provider.generateCalls.Load())
}

// Blocks until cancelled and records the context's error, proving the leader's fetch is bounded
type ctxAwareProvider struct {
	generateCalls atomic.Int32
	started       chan struct{}
	ctxDone       chan error
}

func (p *ctxAwareProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *ctxAwareProvider) GenerateTile(ctx context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	close(p.started)
	<-ctx.Done()
	p.ctxDone <- ctx.Err()
	return nil, ctx.Err()
}

func (p *ctxAwareProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// Regression test for #893: a detached leader must keep the caller's deadline so a hung fetch is still reclaimed
func Test_LayerGroup_RenderTile_LeaderContextKeepsDeadlineAfterCancellation(t *testing.T) {
	provider := &ctxAwareProvider{started: make(chan struct{}), ctxDone: make(chan error, 1)}
	c := &alwaysMissCache{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
	}
	lg := newSingleflightTestLayerGroup(l, c)
	lg.cacheWriteLimiter = make(chan struct{}, maxConcurrentCacheWrites)

	tileRequest := pkg.TileRequest{LayerName: "test", Z: 5, X: 3, Y: 3}

	leaderCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := lg.RenderTile(leaderCtx, tileRequest)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case providerCtxErr := <-provider.ctxDone:
		require.ErrorIs(t, providerCtxErr, context.DeadlineExceeded, "the provider's context should have been cancelled by the original deadline, not left to run forever")
	case <-time.After(time.Second):
		t.Fatal("provider never observed its context being cancelled - the leader's deadline was lost")
	}
}

// Fails its first N calls so a test can prove errors aren't replayed by the dedup mechanism
type failNTimesProvider struct {
	generateCalls atomic.Int32
	failFirstN    int32
}

func (p *failNTimesProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *failNTimesProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	call := p.generateCalls.Add(1)
	if call <= p.failFirstN {
		return nil, errors.New("simulated transient upstream failure")
	}
	return &pkg.Image{Content: []byte("tile")}, nil
}

func (p *failNTimesProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// A request after a failure must trigger a fresh provider call rather than reuse the stale error
func Test_LayerGroup_RenderTile_ErrorIsNotPermanentlyCached(t *testing.T) {
	provider := &failNTimesProvider{failFirstN: 1}
	c := &alwaysMissCache{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
	}
	lg := newSingleflightTestLayerGroup(l, c)
	lg.cacheWriteLimiter = make(chan struct{}, maxConcurrentCacheWrites)

	tileRequest := pkg.TileRequest{LayerName: "test", Z: 5, X: 3, Y: 3}

	_, err := lg.RenderTile(context.Background(), tileRequest)
	require.Error(t, err)

	img, err := lg.RenderTile(context.Background(), tileRequest)
	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, int32(2), provider.generateCalls.Load(), "a request after a failure should trigger a fresh provider call, not replay the cached error")
}

// Fails once released, so every waiter deterministically joins one call before it fails instead of racing to start its own
type blockingUntilReleasedFailingProvider struct {
	generateCalls atomic.Int32
	started       chan struct{}
	release       chan struct{}
}

func (p *blockingUntilReleasedFailingProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *blockingUntilReleasedFailingProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	if p.generateCalls.Add(1) == 1 {
		close(p.started)
	}
	<-p.release
	return nil, errors.New("simulated transient upstream failure")
}

func (p *blockingUntilReleasedFailingProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// Every waiter must see the error, and none may trigger a duplicate call
func Test_LayerGroup_RenderTile_ConcurrentWaitersAllSeeSharedError(t *testing.T) {
	failer := &blockingUntilReleasedFailingProvider{started: make(chan struct{}), release: make(chan struct{})}

	c := &alwaysMissCache{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: failer,
	}
	lg := newSingleflightTestLayerGroup(l, c)
	lg.cacheWriteLimiter = make(chan struct{}, maxConcurrentCacheWrites)

	tileRequest := pkg.TileRequest{LayerName: "test", Z: 5, X: 3, Y: 3}

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = lg.RenderTile(context.Background(), tileRequest)
		}(i)
	}

	<-failer.started
	// Every goroutine is past its cache miss, leaving only a few instructions before it joins the leader
	require.Eventually(t, func() bool { return c.lookupCalls.Load() == n }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	close(failer.release)

	wg.Wait()

	for i := range n {
		require.Error(t, errs[i])
	}
	require.Equal(t, int32(1), failer.generateCalls.Load(), "all concurrent waiters on a failing call should share the single failure, not each retry")
}
