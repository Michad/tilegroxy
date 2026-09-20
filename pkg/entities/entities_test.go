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

package entities

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeCountingProvider proves a real, layer-owned provider gets closed by Entities.Close, going
// through the public LayerGroup constructor since the layer slice itself is unexported.
type closeCountingProvider struct {
	closed *bool
}

func (p closeCountingProvider) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{}, nil
}

func (p closeCountingProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (p closeCountingProvider) Close(_ context.Context) error {
	*p.closed = true
	return nil
}

var closeCountingProviderClosed bool

type closeCountingRegistration struct{}

func (closeCountingRegistration) InitializeConfig() any          { return struct{}{} }
func (closeCountingRegistration) Name() string                   { return "close-counting" }
func (closeCountingRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }
func (closeCountingRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	closeCountingProviderClosed = false
	return closeCountingProvider{closed: &closeCountingProviderClosed}, nil
}

// failingAnalytics implements analytics.Analytics and lifecycle.Closer, always failing to close,
// to exercise the analytics-timeout path in Entities.Close.
type failingAnalytics struct{}

func (failingAnalytics) Record(_ context.Context, _ analytics.Event) error {
	return nil
}

func (failingAnalytics) Close(_ context.Context) error {
	return errors.New("analytics did not finish flushing")
}

func Test_Entities_CloseNil(t *testing.T) {
	var e *Entities

	require.NoError(t, e.Close(context.Background()))
}

// Entities is exported and constructed directly, so a generation missing any given entity has to close
// without panicking on the nil field.
func Test_Entities_CloseWithUnsetEntities(t *testing.T) {
	e := &Entities{}

	assert.NotPanics(t, func() {
		require.NoError(t, e.Close(context.Background()))
	})
}

func Test_Entities_CloseIsIdempotent(t *testing.T) {
	e := &Entities{}

	require.NoError(t, e.Close(context.Background()))
	require.NoError(t, e.Close(context.Background()))
}

// Providers close before analytics: they hold nothing the analytics flush depends on, and a CGI
// child process is worth reaping early. Uses a LayerGroup constructed through the public
// constructor since its provider-holding field is unexported outside the layer package.
func Test_Entities_ClosesLayerGroupProviders(t *testing.T) {
	layer.RegisterProvider(closeCountingRegistration{})
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{{ID: "l", Provider: map[string]any{"name": "close-counting"}}}
	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	e := &Entities{LayerGroup: lg}

	require.NoError(t, e.Close(context.Background()))
	assert.True(t, closeCountingProviderClosed)
}

// closableAuth stands in for a custom auth script that owns resources of its own
type closableAuth struct {
	closed *bool
}

func (a closableAuth) CheckAuthentication(_ context.Context, _ *http.Request) bool { return true }

func (a closableAuth) Close(_ context.Context) error {
	*a.closed = true
	return nil
}

func Test_Entities_ClosesAuth(t *testing.T) {
	var closed bool
	e := &Entities{Auth: closableAuth{closed: &closed}}

	require.NoError(t, e.Close(context.Background()))
	assert.True(t, closed, "a custom auth script's close hook has to run on shutdown")
}

func Test_Entities_ClosesAuthEvenWhenAnalyticsTimesOut(t *testing.T) {
	var closed bool
	e := &Entities{
		Auth:      closableAuth{closed: &closed},
		Analytics: &analytics.AnalyticsWrapper{Name: "failing", ID: "failing", Analytics: failingAnalytics{}},
	}

	require.Error(t, e.Close(context.Background()))
	assert.True(t, closed, "auth closes before the flush, so a stalled flush must not strand it")
}

// The analytics-timeout early return that leaves datastores open must survive the new LayerGroup
// step being added ahead of it, and the LayerGroup step must still run even though analytics
// times out - the two errors are independent and both get joined.
func Test_Entities_AnalyticsTimeoutStillLeavesDatastoresOpen(t *testing.T) {
	e := &Entities{
		LayerGroup: &layer.LayerGroup{},
		Analytics:  &analytics.AnalyticsWrapper{Name: "failing", ID: "failing", Analytics: failingAnalytics{}},
	}

	err := e.Close(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "analytics did not finish flushing")
}

// deadlineRecordingAnalytics records how much of the shutdown budget was left when its flush ran,
// so the ordering of the phases ahead of it becomes observable.
type deadlineRecordingAnalytics struct {
	remaining *time.Duration
}

func (deadlineRecordingAnalytics) Record(_ context.Context, _ analytics.Event) error {
	return nil
}

func (a deadlineRecordingAnalytics) Close(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		*a.remaining = time.Until(deadline)
	}

	return nil
}

// slowSaveCache holds its write open long enough that a drain waiting on it would visibly eat into
// whatever phase runs after the wait.
type slowSaveCache struct{ delay time.Duration }

func (slowSaveCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c slowSaveCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	time.Sleep(c.delay)
	return nil
}

func (slowSaveCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

// renderingProvider returns a real image so the render reaches the cache write path.
type renderingProvider struct{}

func (renderingProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (renderingProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{Content: []byte("tile"), ContentType: "image/png"}, nil
}

type renderingRegistration struct{}

func (renderingRegistration) InitializeConfig() any          { return struct{}{} }
func (renderingRegistration) Name() string                   { return "rendering" }
func (renderingRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }
func (renderingRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return renderingProvider{}, nil
}

type slowSaveCacheRegistration struct{}

func (slowSaveCacheRegistration) InitializeConfig() any { return struct{}{} }
func (slowSaveCacheRegistration) Name() string          { return "slow-save" }
func (slowSaveCacheRegistration) Initialize(_ any, _ cache.CacheDeps) (cache.Cache, error) {
	return slowSaveCache{delay: 300 * time.Millisecond}, nil
}

// The analytics flush runs on a reserved slice of the shutdown budget. Waiting on background cache
// writes ahead of it would spend that reserve on a slow cache backend and drop batched events, so
// the wait belongs after the flush, next to the caches it actually guards.
func Test_Entities_CacheWriteDrainDoesNotSpendTheAnalyticsReserve(t *testing.T) {
	cache.RegisterCache(slowSaveCacheRegistration{})
	layer.RegisterProvider(renderingRegistration{})

	cfg := config.DefaultConfig()
	cfg.Cache = map[string]any{"name": "slow-save"}
	cfg.Layers = []config.LayerConfig{
		{ID: "test", Provider: map[string]any{"name": "rendering"}},
	}

	caches, err := cache.ConstructCacheRegistry(cfg.Cache, cfg.DefaultCache, nil, cache.CacheDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	lg, err := layer.ConstructLayerGroup(cfg, caches, nil, nil)
	require.NoError(t, err)

	// Leaves a cache write in flight for the drain to find.
	_, err = lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	var remaining time.Duration
	e := &Entities{
		LayerGroup: lg,
		Caches:     caches,
		Analytics:  &analytics.AnalyticsWrapper{Name: "recording", ID: "recording", Analytics: deadlineRecordingAnalytics{remaining: &remaining}},
	}

	const budget = 2 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	require.NoError(t, e.Close(ctx))

	// The 300ms write is still in flight when analytics flushes, so nearly the whole budget is left.
	assert.Greater(t, remaining, budget-100*time.Millisecond,
		"the analytics flush must not be charged for a slow cache write")
}
