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
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

// recordingAnalytics captures the events the tile handler emits.
type recordingAnalytics struct {
	mutex  sync.Mutex
	events []analytics.Event
}

func (r *recordingAnalytics) Record(_ context.Context, event analytics.Event) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.events = append(r.events, event)

	return nil
}

func (r *recordingAnalytics) snapshot() []analytics.Event {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	out := make([]analytics.Event, len(r.events))
	copy(out, r.events)

	return out
}

type recordingRegistration struct {
	instance *recordingAnalytics
}

func (s recordingRegistration) InitializeConfig() any { return analytics.CommonConfig{} }
func (s recordingRegistration) Name() string          { return "servertestrecorder" }

func (s recordingRegistration) Initialize(_ any, _ analytics.AnalyticsDeps) (analytics.Analytics, error) {
	return s.instance, nil
}

// setupAnalyticsHandler builds a tile handler serving a static layer, wired to a recording
// analytics module. fields configures which extra attributes the module asks for.
func setupAnalyticsHandler(t *testing.T, layers []config.LayerConfig, fields []string) (*tileHandler, *recordingAnalytics) {
	t.Helper()

	rec := &recordingAnalytics{}
	analytics.RegisterAnalytics(recordingRegistration{instance: rec})

	cfg := config.DefaultConfig()
	cfg.Layers = layers

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}

	lg, err := layer.ConstructLayerGroup(cfg, c, nil, nil)
	require.NoError(t, err)

	moduleCfg := map[string]interface{}{"name": "servertestrecorder"}
	if fields != nil {
		moduleCfg["fields"] = fields
	}

	a, err := analytics.ConstructAnalytics(moduleCfg, nil, analytics.AnalyticsDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	handler, err := newTileHandler(reloadableEntities{config: &cfg, auth: auth, layerGroup: lg, analytics: a})
	require.NoError(t, err)

	return &handler, rec
}

func staticLayer(id string, skipAnalytics bool) config.LayerConfig {
	return config.LayerConfig{
		ID:            id,
		SkipAnalytics: skipAnalytics,
		Provider:      map[string]interface{}{"name": "static", "color": "FFF"},
	}
}

func doTileRequest(t *testing.T, handler *tileHandler, layerName, z, x, y string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/"+layerName+"/"+z+"/"+x+"/"+y, nil).
		WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", layerName)
	req.SetPathValue("z", z)
	req.SetPathValue("x", x)
	req.SetPathValue("y", y)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })

	return resp
}

func Test_TileHandler_Analytics_SuccessEmitsEvent(t *testing.T) {
	handler, rec := setupAnalyticsHandler(t, []config.LayerConfig{staticLayer("main", false)}, nil)

	resp := doTileRequest(t, handler, "main", "8", "12", "32")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	events := rec.snapshot()
	require.Len(t, events, 1)

	assert.Equal(t, "main", events[0].LayerID)
	assert.Equal(t, "main", events[0].LayerName)
	assert.Equal(t, 8, events[0].Z)
	assert.Equal(t, 12, events[0].X)
	assert.Equal(t, 32, events[0].Y)
	assert.Empty(t, events[0].UserID, "an unauthenticated request has no user")
	assert.False(t, events[0].Time.IsZero())
}

func Test_TileHandler_Analytics_SkipAnalyticsLayer(t *testing.T) {
	handler, rec := setupAnalyticsHandler(t, []config.LayerConfig{staticLayer("quiet", true)}, nil)

	resp := doTileRequest(t, handler, "quiet", "8", "12", "32")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Empty(t, rec.snapshot(), "a layer with skipAnalytics set must not produce events")
}

func Test_TileHandler_Analytics_NotEmittedOnError(t *testing.T) {
	handler, rec := setupAnalyticsHandler(t, []config.LayerConfig{staticLayer("main", false)}, nil)

	// A layer that doesn't exist fails before a tile is produced.
	resp := doTileRequest(t, handler, "nonexistent", "8", "12", "32")
	require.NotEqual(t, http.StatusOK, resp.StatusCode)

	assert.Empty(t, rec.snapshot(), "analytics reflects successful usage only")
}

func Test_TileHandler_Analytics_NotEmittedOnBadCoordinates(t *testing.T) {
	handler, rec := setupAnalyticsHandler(t, []config.LayerConfig{staticLayer("main", false)}, nil)

	resp := doTileRequest(t, handler, "main", "8", "notanumber", "32")
	require.NotEqual(t, http.StatusOK, resp.StatusCode)

	assert.Empty(t, rec.snapshot())
}

func Test_TileHandler_Analytics_ResolvesConfiguredFields(t *testing.T) {
	handler, rec := setupAnalyticsHandler(t,
		[]config.LayerConfig{staticLayer("main", false)},
		[]string{"contenttype", "bytes", "layername"},
	)

	resp := doTileRequest(t, handler, "main", "8", "12", "32")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	events := rec.snapshot()
	require.Len(t, events, 1)

	assert.Equal(t, "main", events[0].Fields["layername"])
	assert.NotEmpty(t, events[0].Fields["contenttype"])

	bytesField, ok := events[0].Fields["bytes"].(int)
	require.True(t, ok, "bytes should resolve to an int")
	assert.Positive(t, bytesField, "the recorded size should match the served tile")
}

func Test_TileHandler_Analytics_PatternLayerRecordsID(t *testing.T) {
	// With a pattern the URL name and the configured ID differ; analytics should record the ID so
	// events line up with the per-layer telemetry metrics.
	layerCfg := config.LayerConfig{
		ID:       "patterned",
		Pattern:  "tile-{color}",
		Provider: map[string]interface{}{"name": "static", "color": "FFF"},
	}

	handler, rec := setupAnalyticsHandler(t, []config.LayerConfig{layerCfg}, []string{"layername"})

	resp := doTileRequest(t, handler, "tile-red", "8", "12", "32")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	events := rec.snapshot()
	require.Len(t, events, 1)

	assert.Equal(t, "patterned", events[0].LayerID)
	assert.Equal(t, "tile-red", events[0].LayerName)
	assert.Equal(t, "tile-red", events[0].Fields["layername"])
}

func Test_TileHandler_Analytics_NoModulesConfigured(t *testing.T) {
	// The common case: no analytics section at all. The handler must take the fast path without
	// tripping over a nil registry.
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayer("main", false)}

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}

	lg, err := layer.ConstructLayerGroup(cfg, c, nil, nil)
	require.NoError(t, err)

	handler, err := newTileHandler(reloadableEntities{config: &cfg, auth: auth, layerGroup: lg})
	require.NoError(t, err)

	resp := doTileRequest(t, &handler, "main", "8", "12", "32")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// closeTracker records how many times a generation was released so a reload can be checked for both
// double-closing the old generation and abandoning the live one.
type closeTracker struct {
	closes atomic.Int64
}

func (c *closeTracker) Record(_ context.Context, _ analytics.Event) error { return nil }

func (c *closeTracker) Close(_ context.Context) error {
	c.closes.Add(1)
	return nil
}

type closeTrackerRegistration struct {
	instance *closeTracker
}

func (s closeTrackerRegistration) InitializeConfig() any { return analytics.CommonConfig{} }
func (s closeTrackerRegistration) Name() string          { return "servertestcloser" }

func (s closeTrackerRegistration) Initialize(_ any, _ analytics.AnalyticsDeps) (analytics.Analytics, error) {
	return s.instance, nil
}

// generationFor builds an Entities holding whichever closeTracker was most recently registered, which
// reports when that generation is closed.
func generationFor(t *testing.T) (*config.Config, *entities.Entities) {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayer("main", false)}

	var c cache.Cache = caches.Noop{}

	lg, err := layer.ConstructLayerGroup(cfg, c, nil, nil)
	require.NoError(t, err)

	a, err := analytics.ConstructAnalytics(
		map[string]interface{}{"name": "servertestcloser"}, nil, analytics.AnalyticsDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	return &cfg, &entities.Entities{
		LayerGroup: lg,
		Auth:       authentications.Noop{},
		Analytics:  a,
		Cache:      c,
	}
}

func Test_TileHandler_ReloadReleasesOldGenerationOnce(t *testing.T) {
	oldTracker := &closeTracker{}
	newTracker := &closeTracker{}

	analytics.RegisterAnalytics(closeTrackerRegistration{instance: oldTracker})
	oldCfg, oldEnt := generationFor(t)

	// Server.Timeout drives the grace period before the old generation is released, so keep it at zero
	// to avoid the test waiting on it.
	oldCfg.Server.Timeout = 0

	handler, err := newTileHandler(newReloadableEntities(oldCfg, oldEnt))
	require.NoError(t, err)

	analytics.RegisterAnalytics(closeTrackerRegistration{instance: newTracker})
	newCfg, newEnt := generationFor(t)
	newCfg.Server.Timeout = 0

	handler.reloadEntities(newReloadableEntities(newCfg, newEnt))

	assert.Eventually(t, func() bool {
		return oldTracker.closes.Load() == 1
	}, 5*time.Second, 20*time.Millisecond, "the superseded generation should be released exactly once")

	// The generation now serving must still be open.
	require.Equal(t, int64(0), newTracker.closes.Load(), "the live generation must not be closed by a reload")

	// Shutdown closes whatever is currently serving, which is the new generation, not the one the
	// server was originally handed.
	require.NoError(t, handler.currentEntities().Close(context.Background()))
	assert.Equal(t, int64(1), newTracker.closes.Load())
	assert.Equal(t, int64(1), oldTracker.closes.Load(), "the old generation must not be closed a second time")
}
