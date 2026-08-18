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

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/authentications"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingAnalytics is an analytics.Analytics whose Close blocks until told to proceed, or returns
// a configured error, so tests can drive generation close behavior deterministically instead of
// relying on sleeps
type blockingAnalytics struct {
	release  chan struct{}
	closeErr error
}

func (b *blockingAnalytics) Record(_ context.Context, _ analytics.Event) error {
	return nil
}

func (b *blockingAnalytics) Close(ctx context.Context) error {
	if b.release != nil {
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return b.closeErr
}

// entitiesWithAnalytics builds an *entities.Entities whose Close is driven entirely by the given
// Analytics implementation, so a test can control blocking or errors deterministically
func entitiesWithAnalytics(a analytics.Analytics) *entities.Entities {
	return &entities.Entities{Analytics: &analytics.AnalyticsWrapper{Name: "test", Analytics: a}}
}

func Test_GenerationClosesWhenIdle(t *testing.T) {
	g := newGeneration(&entities.Entities{})

	// No in-flight requests, so marking it closing releases it after the floor.
	g.markClosing(context.Background(), time.Millisecond)

	require.Eventually(t, g.isClosed, time.Second, 10*time.Millisecond,
		"an idle generation must release without waiting on a fixed timeout")
}

func Test_GenerationWaitsForInFlightRequest(t *testing.T) {
	g := newGeneration(&entities.Entities{})

	g.acquire()

	g.markClosing(context.Background(), time.Millisecond)

	// The held request pins the generation; releasing it early would tear down connections
	// underneath a request that is still using them.
	time.Sleep(50 * time.Millisecond)
	assert.False(t, g.isClosed(), "generation must stay open while a request holds it")
	assert.Equal(t, 1, g.inFlight(), "the held request must still be counted")
	assert.Equal(t, 0, g.closeCount(), "close must not have run while pinned")

	g.release()

	require.Eventually(t, g.isClosed, time.Second, 10*time.Millisecond,
		"the last release must close the generation")
}

func Test_GenerationClosesOnlyOnce(t *testing.T) {
	g := newGeneration(&entities.Entities{})

	g.acquire()
	g.acquire()
	g.markClosing(context.Background(), time.Millisecond)

	g.release()
	g.release()

	require.Eventually(t, g.isClosed, time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, g.closeCount(), "Close must not run twice")
}

func Test_GenerationConcurrentAcquireRelease(t *testing.T) {
	g := newGeneration(&entities.Entities{})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			g.acquire()
			time.Sleep(time.Millisecond)
			g.release()
		}()
	}

	g.markClosing(context.Background(), time.Millisecond)
	wg.Wait()

	require.Eventually(t, g.isClosed, time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, g.closeCount())
}

func Test_RegistryClosesEveryLiveGeneration(t *testing.T) {
	reg := newGenerationRegistry()

	g1 := newGeneration(&entities.Entities{})
	g2 := newGeneration(&entities.Entities{})
	reg.add(g1)
	reg.add(g2)

	// Shutdown must reach a generation a recent reload swapped out, not just the current one.
	require.NoError(t, reg.closeAll(context.Background()))

	assert.True(t, g1.isClosed())
	assert.True(t, g2.isClosed())
}

func Test_RegistryDoesNotRetainClosedGenerations(t *testing.T) {
	reg := newGenerationRegistry()

	gens := make([]*generation, 0, 5)
	for range 5 {
		g := newGeneration(&entities.Entities{})
		reg.add(g)
		gens = append(gens, g)
	}

	require.Equal(t, 5, reg.liveCount())

	for _, g := range gens {
		g.markClosing(context.Background(), time.Millisecond)
	}

	for _, g := range gens {
		require.Eventually(t, g.isClosed, time.Second, 10*time.Millisecond,
			"every generation must finish closing before the registry is checked")
	}

	require.Eventually(t, func() bool { return reg.liveCount() == 0 }, time.Second, 10*time.Millisecond,
		"a closed generation must be removed from the registry instead of pinning its entities forever")
}

func Test_CloseAllWaitsForInFlightRequest(t *testing.T) {
	reg := newGenerationRegistry()

	g := newGeneration(&entities.Entities{})
	reg.add(g)

	g.acquire()

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- reg.closeAll(ctx)
	}()

	// closeAll must not close a generation with a live reference out from under the request.
	time.Sleep(50 * time.Millisecond)
	assert.False(t, g.isClosed(), "closeAll must wait rather than force-close while refs are held")

	g.release()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("closeAll did not return after the in-flight request released")
	}

	assert.True(t, g.isClosed())
}

func Test_CloseAllJoinsInProgressClose(t *testing.T) {
	reg := newGenerationRegistry()

	block := &blockingAnalytics{release: make(chan struct{})}
	g := newGeneration(entitiesWithAnalytics(block))
	reg.add(g)

	// Start a close directly, as release() or the floor goroutine would, and have it block
	// mid-drain so closeAll races an in-progress close rather than a fresh one.
	closeNowDone := make(chan struct{})
	go func() {
		_ = g.closeNow(context.Background())
		close(closeNowDone)
	}()

	require.Eventually(t, func() bool {
		return g.closeCount() > 0
	}, time.Second, time.Millisecond, "closeNow must have started")

	closeAllDone := make(chan error, 1)
	go func() {
		closeAllDone <- reg.closeAll(context.Background())
	}()

	// While the drain is blocked, closeAll must not have returned.
	time.Sleep(50 * time.Millisecond)
	select {
	case <-closeAllDone:
		t.Fatal("closeAll returned while the in-progress close was still draining")
	default:
	}

	assert.False(t, g.isClosed(), "isClosed must reflect the drain still being in progress")

	close(block.release)

	select {
	case err := <-closeAllDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("closeAll did not join the in-progress close after it finished draining")
	}

	<-closeNowDone
	assert.True(t, g.isClosed())
	assert.Equal(t, 1, g.closeCount(), "the drain must not have been run twice")
}

func Test_CloseAllReturnsCloseError(t *testing.T) {
	reg := newGenerationRegistry()

	failErr := errors.New("boom")
	g := newGeneration(entitiesWithAnalytics(&blockingAnalytics{closeErr: failErr}))
	reg.add(g)

	err := reg.closeAll(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, failErr)
}

func Test_ReloadKeepsGenerationAliveForInFlightRequest(t *testing.T) {
	reg := newGenerationRegistry()
	oldGen := newGeneration(&entities.Entities{})
	reg.add(oldGen)

	cfg := config.DefaultConfig()
	handler, err := newTileHandler(newReloadableEntities(&cfg, oldGen.all, oldGen))
	require.NoError(t, err)

	// Simulate a request that read the pointer and is still running.
	handler.entityMutex.RLock()
	inFlight := handler.entities
	inFlight.gen.acquire()
	handler.entityMutex.RUnlock()

	newGen := newGeneration(&entities.Entities{})
	reg.add(newGen)
	handler.reloadEntities(newReloadableEntities(&cfg, newGen.all, newGen))

	time.Sleep(50 * time.Millisecond)
	assert.False(t, oldGen.isClosed(), "the old generation must outlive the request holding it")

	inFlight.gen.release()

	require.Eventually(t, oldGen.isClosed, 5*time.Second, 10*time.Millisecond,
		"the old generation must release once its last request returns")
	assert.False(t, newGen.isClosed(), "the serving generation must stay open")
}

// Test_ServeHTTPReleasesGenerationRef exercises the real acquire/release path end to end: a
// request through ServeHTTP must not leave the generation's refcount pinned above zero once the
// response has been written.
func Test_ServeHTTPReleasesGenerationRef(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{{ID: "main", Provider: map[string]interface{}{"name": "static", "color": "FFF"}}}

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}

	lg, err := layer.ConstructLayerGroup(cfg, c, nil, nil)
	require.NoError(t, err)

	gen := newGeneration(&entities.Entities{LayerGroup: lg, Auth: auth, Cache: c})

	handler, err := newTileHandler(newReloadableEntities(&cfg, gen.all, gen))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/main/8/12/32", nil).
		WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "main")
	req.SetPathValue("z", "8")
	req.SetPathValue("x", "12")
	req.SetPathValue("y", "32")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Result().StatusCode) //nolint:bodyclose // httptest recorder body needs no closing

	assert.Equal(t, 0, gen.inFlight(), "ServeHTTP must release its hold on the generation once the response is written")
}

func Test_CurrentEntitiesFollowsReload(t *testing.T) {
	reg := newGenerationRegistry()
	oldGen := newGeneration(&entities.Entities{})
	newGen := newGeneration(&entities.Entities{})
	reg.add(oldGen)

	cfg := config.DefaultConfig()
	handler, err := newTileHandler(newReloadableEntities(&cfg, oldGen.all, oldGen))
	require.NoError(t, err)

	reg.add(newGen)
	handler.reloadEntities(newReloadableEntities(&cfg, newGen.all, newGen))

	// Shutdown closes whatever this returns, so it must track the swap.
	assert.Same(t, newGen.all, handler.currentEntities())
}
