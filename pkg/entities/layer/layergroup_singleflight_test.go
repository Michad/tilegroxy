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

func newSingleflightTestLayerGroup(l *Layer, c *alwaysMissCache) *LayerGroup {
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}
	l.allowCoalesce = true
	l.Cache = c

	return &LayerGroup{
		layers:           []*Layer{l},
		DefaultCache:     c,
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}
}

// N concurrent requests for the identical tile key must collapse into exactly one provider call,
// with every caller receiving the same successful result.
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

// Requests for genuinely different tile keys must not be serialized against each other by the
// dedup mechanism - only identical keys should share a single in-flight call.
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
	// If distinct keys were serialized against each other this would take roughly n*delay. Allow
	// generous headroom above a single delay to keep this robust under sandbox scheduling jitter.
	require.Less(t, elapsed, 5*time.Duration(n/2)*provider.delay, "requests for distinct tiles appear to be serialized rather than running concurrently")
}

// blockingUntilReleasedProvider blocks in GenerateTile until the test explicitly releases it,
// so a test can deterministically keep a "leader" call in flight while a waiter's own context
// expires.
type blockingUntilReleasedProvider struct {
	generateCalls atomic.Int32
	started       chan struct{}
	release       chan struct{}
}

func (p *blockingUntilReleasedProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *blockingUntilReleasedProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	p.generateCalls.Add(1)
	close(p.started)
	<-p.release
	return &pkg.Image{Content: []byte("tile")}, nil
}

func (p *blockingUntilReleasedProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// A waiter's own context deadline must be able to expire independently while the leader is still
// fetching - it must not block until the leader finishes, and it must not cancel the leader or
// other waiters.
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

	// Leader: starts the fetch and blocks in the provider until released.
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		_, _ = lg.RenderTile(context.Background(), tileRequest)
	}()

	<-provider.started // leader is now inside GenerateTile, blocked on release

	// Waiter: joins the same in-flight key but with a short deadline that will expire long before
	// the leader is released.
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

	// Release the leader and let it finish so the test cleans up without leaking goroutines.
	close(provider.release)
	<-leaderDone
	require.Equal(t, int32(1), provider.generateCalls.Load())
}

// failNTimesProvider fails its first N calls then succeeds, so a test can verify an error result
// isn't permanently cached/replayed by the dedup mechanism.
type failNTimesProvider struct {
	generateCalls atomic.Int32
	failFirstN    int32
}

func (p *failNTimesProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *failNTimesProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	call := p.generateCalls.Add(1)
	if call <= p.failFirstN {
		return nil, errors.New("simulated transient upstream failure")
	}
	return &pkg.Image{Content: []byte("tile")}, nil
}

func (p *failNTimesProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// A failed generation must not be poisoned/replayed forever: a request after a failed one should
// trigger a fresh provider call rather than instantly reusing the stale error.
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

// blockingUntilReleasedFailingProvider is like blockingUntilReleasedProvider but returns an error
// once released instead of an image, so a test can deterministically gather every waiter onto one
// in-flight call before it fails - a real race would let some late-arriving goroutines miss the
// window and start their own (successful) call instead of joining, which isn't what this test
// means to exercise.
type blockingUntilReleasedFailingProvider struct {
	generateCalls atomic.Int32
	started       chan struct{}
	release       chan struct{}
}

func (p *blockingUntilReleasedFailingProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	providerContext.AuthBypass = true
	return providerContext, nil
}

func (p *blockingUntilReleasedFailingProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	if p.generateCalls.Add(1) == 1 {
		close(p.started)
	}
	<-p.release
	return nil, errors.New("simulated transient upstream failure")
}

func (p *blockingUntilReleasedFailingProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// Concurrent waiters that join an in-flight call which ultimately fails must all observe the
// error (not a subset succeeding), and none of them should trigger their own duplicate call.
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

	<-failer.started // the leader is now blocked in GenerateTile; every waiter has had time to queue behind it
	time.Sleep(20 * time.Millisecond)
	close(failer.release)

	wg.Wait()

	for i := range n {
		require.Error(t, errs[i])
	}
	require.Equal(t, int32(1), failer.generateCalls.Load(), "all concurrent waiters on a failing call should share the single failure, not each retry")
}
