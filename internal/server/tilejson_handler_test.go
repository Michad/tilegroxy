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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/authentications"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTileJSONTestEntities(t *testing.T, cfg config.Config) reloadableEntities {
	t.Helper()

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(cfg, c, nil, nil)
	require.NoError(t, err)

	return reloadableEntities{config: &cfg, auth: auth, layerGroup: lg}
}

func staticLayerConfig(id string) config.LayerConfig {
	return config.LayerConfig{ID: id, Provider: map[string]interface{}{"name": "static", "color": "FFF"}}
}

func Test_TileJSONHandler_ReloadEntities_SwapsGenerationAndReleasesOld(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true

	oldGen := newGeneration(&entities.Entities{})
	newGen := newGeneration(&entities.Entities{})

	h := newTileJSONHandler(newReloadableEntities(&cfg, oldGen.all, oldGen), true)

	h.entityMutex.RLock()
	before := h.entities
	h.entityMutex.RUnlock()
	assert.Same(t, oldGen, before.gen)

	h.reloadEntities(newReloadableEntities(&cfg, newGen.all, newGen))

	h.entityMutex.RLock()
	after := h.entities
	h.entityMutex.RUnlock()
	assert.Same(t, newGen, after.gen)

	require.Eventually(t, oldGen.isClosed, 5*time.Second, 10*time.Millisecond,
		"reloadEntities must release the superseded generation")
	assert.False(t, newGen.isClosed(), "the serving generation must stay open")
}

func Test_TileJSONHandlers_ReloadEntities_Disabled_NoOp(t *testing.T) {
	cfg := config.DefaultConfig()

	th := setupTileJSONHandlers(&cfg, reloadableEntities{})
	gen := newGeneration(&entities.Entities{})

	// Must not panic even though TileJSON is disabled and no handlers were built.
	th.reloadEntities(&cfg, gen.all, gen)
	th.wrapWithTelemetry()

	mux := &http.ServeMux{}
	th.registerRoutes(mux)

	assert.Nil(t, th.index, "disabled TileJSON must not build a handler")
}

func Test_TileJSONHandlers_WrapWithTelemetry_PreservesRouting(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestEntities(t, cfg)
	th := setupTileJSONHandlers(&cfg, ent)
	th.wrapWithTelemetry()

	mux := &http.ServeMux{}
	th.registerRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "http://example.com"+th.indexPath, nil).WithContext(pkg.BackgroundContext())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Result().StatusCode) //nolint:bodyclose // httptest recorder body needs no closing
}

func Test_TileJSONHandler_Index_ListsEligibleLayers(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Layers = []config.LayerConfig{
		staticLayerConfig("main"),
		{
			ID:       "pattern1",
			Pattern:  "my_{name}_{version}",
			Examples: []string{"my_foo_v1", "my_bar_v2"},
			Provider: map[string]interface{}{"name": "static", "color": "FFF"},
		},
		{
			ID:       "pattern2",
			Pattern:  "other_{name}",
			Provider: map[string]interface{}{"name": "static", "color": "FFF"},
		},
	}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, true)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tilejson.json", nil).WithContext(pkg.BackgroundContext())
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	var entries []tileJSONIndexEntry
	require.NoError(t, json.NewDecoder(res.Body).Decode(&entries))

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}

	// pattern2 has no examples, so it's excluded entirely
	assert.ElementsMatch(t, []string{"main", "my_foo_v1", "my_bar_v2"}, names)
}

func Test_TileJSONHandler_Document_PlainLayer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	minZoom := 2
	maxZoom := 18
	layerCfg := staticLayerConfig("main")
	layerCfg.MinZoom = &minZoom
	layerCfg.MaxZoom = &maxZoom
	layerCfg.Description = "desc"
	layerCfg.Attribution = "attr"
	cfg.Layers = []config.LayerConfig{layerCfg}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, false)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/main.json", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layerjson", "main.json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	var doc layer.TileJSONDocument
	require.NoError(t, json.NewDecoder(res.Body).Decode(&doc))

	assert.Equal(t, "main", doc.Name)
	assert.Equal(t, "3.0.0", doc.TileJSON)
	assert.Equal(t, 2, doc.MinZoom)
	assert.Equal(t, 18, doc.MaxZoom)
	assert.Equal(t, "desc", doc.Description)
	assert.Equal(t, "attr", doc.Attribution)
	require.Len(t, doc.Tiles, 1)
	assert.Equal(t, "http://example.com/tiles/main/{z}/{x}/{y}", doc.Tiles[0])
}

func Test_TileJSONHandler_Document_UnknownLayer_Returns404(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, false)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/doesnotexist.json", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layerjson", "doesnotexist.json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func Test_TileJSONHandler_Document_PatternLayerWithoutExamples_NotEligible(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Layers = []config.LayerConfig{
		{ID: "id1", Pattern: "my_{name}", Provider: map[string]interface{}{"name": "static", "color": "FFF"}},
	}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, false)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/my_foo.json", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layerjson", "my_foo.json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func Test_TileJSONHandler_LayerScope_RestrictsIndexAndDocument(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main"), staticLayerConfig("other")}

	ent := buildTileJSONTestEntities(t, cfg)
	indexHandler := newTileJSONHandler(ent, true)
	docHandler := newTileJSONHandler(ent, false)

	ctx := pkg.BackgroundContext()
	limitLayers, _ := pkg.LimitLayersFromContext(ctx)
	*limitLayers = true
	allowedLayers, _ := pkg.AllowedLayersFromContext(ctx)
	*allowedLayers = []string{"main"}

	// Index only lists the allowed layer
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/tilejson.json", nil).WithContext(ctx)
	w1 := httptest.NewRecorder()
	indexHandler.ServeHTTP(w1, req1)
	res1 := w1.Result()
	defer func() { require.NoError(t, res1.Body.Close()) }()

	var entries []tileJSONIndexEntry
	require.NoError(t, json.NewDecoder(res1.Body).Decode(&entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "main", entries[0].Name)

	// Requesting the disallowed layer's document directly returns 404, not 401: the caller is
	// authenticated, just out of scope for this layer, and the scoped-out layer shouldn't be
	// distinguishable from one that was never configured. See issue #766.
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/other.json", nil).WithContext(ctx)
	req2.SetPathValue("layerjson", "other.json")
	w2 := httptest.NewRecorder()
	docHandler.ServeHTTP(w2, req2)
	res2 := w2.Result()
	defer func() { require.NoError(t, res2.Body.Close()) }()
	assert.Equal(t, http.StatusNotFound, res2.StatusCode)

	// Requesting the allowed layer's document succeeds
	req3 := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/main.json", nil).WithContext(ctx)
	req3.SetPathValue("layerjson", "main.json")
	w3 := httptest.NewRecorder()
	docHandler.ServeHTTP(w3, req3)
	res3 := w3.Result()
	defer func() { require.NoError(t, res3.Body.Close()) }()
	assert.Equal(t, http.StatusOK, res3.StatusCode)
}

func Test_TileJSONHandler_AllowedArea_IntersectsBounds(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	layerCfg := staticLayerConfig("main")
	layerCfg.Bounds = config.BoundsConfig{South: -10, North: 10, West: -10, East: 10}
	layerCfg.DataType = config.DataTypeRaster
	cfg.Layers = []config.LayerConfig{layerCfg}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, false)

	ctx := pkg.BackgroundContext()
	ctxAllowedArea, _ := pkg.AllowedAreaFromContext(ctx)
	*ctxAllowedArea = pkg.Bounds{South: -5, North: 5, West: -5, East: 20, SRID: pkg.SRIDWGS84}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/main.json", nil).WithContext(ctx)
	req.SetPathValue("layerjson", "main.json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	var doc layer.TileJSONDocument
	require.NoError(t, json.NewDecoder(res.Body).Decode(&doc))
	assert.Equal(t, []float64{-5, -5, 10, 5}, doc.Bounds)
}

func Test_TileJSONHandler_BaseURLs_Override(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Server.TileJSON.BaseURLs = []string{"https://tiles.example.com/maps"}
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, false)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/tiles/main.json", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layerjson", "main.json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	var doc layer.TileJSONDocument
	require.NoError(t, json.NewDecoder(res.Body).Decode(&doc))
	require.Len(t, doc.Tiles, 1)
	assert.Equal(t, "https://tiles.example.com/maps/tiles/main/{z}/{x}/{y}", doc.Tiles[0])
}

func Test_TileJSONHandler_BaseURLs_Multiple(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Server.TileJSON.BaseURLs = []string{"https://tiles-a.example.com", "https://tiles-b.example.com/maps"}
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, false)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/tiles/main.json", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layerjson", "main.json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	var doc layer.TileJSONDocument
	require.NoError(t, json.NewDecoder(res.Body).Decode(&doc))
	require.Len(t, doc.Tiles, 2)
	assert.Equal(t, "https://tiles-a.example.com/tiles/main/{z}/{x}/{y}", doc.Tiles[0])
	assert.Equal(t, "https://tiles-b.example.com/maps/tiles/main/{z}/{x}/{y}", doc.Tiles[1])
}

func Test_TileJSONHandler_ForwardedHeaders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestEntities(t, cfg)
	h := newTileJSONHandler(ent, false)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/tiles/main.json", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layerjson", "main.json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "public.example.com")
	req.Header.Set("X-Forwarded-Prefix", "/maps")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	var doc layer.TileJSONDocument
	require.NoError(t, json.NewDecoder(res.Body).Decode(&doc))
	require.Len(t, doc.Tiles, 1)
	assert.Equal(t, "https://public.example.com/maps/tiles/main/{z}/{x}/{y}", doc.Tiles[0])
}

// setupTestRootHandler builds the full mux via setupHandlers, discarding the reload/shutdown
// plumbing tests here don't exercise.
func setupTestRootHandler(cfg *config.Config, ent *entities.Entities) (http.Handler, error) {
	rootHandler, _, currentEntities, registry, closeAccessLog, err := setupHandlers(cfg, ent)
	_ = currentEntities
	_ = registry
	_ = closeAccessLog

	return rootHandler, err
}

func Test_SetupHandlers_TileJSON_RoutesRegistered(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.TileJSON.Enabled = true
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(cfg, c, nil, nil)
	require.NoError(t, err)

	ent := &entities.Entities{LayerGroup: lg, Auth: auth}

	rootHandler, err := setupTestRootHandler(&cfg, ent)
	require.NoError(t, err)

	ts := httptest.NewServer(rootHandler)
	defer ts.Close()

	resIndex, err := http.Get(ts.URL + "/tilejson.json")
	require.NoError(t, err)
	defer func() { require.NoError(t, resIndex.Body.Close()) }()
	assert.Equal(t, http.StatusOK, resIndex.StatusCode)

	var indexEntries []tileJSONIndexEntry
	require.NoError(t, json.NewDecoder(resIndex.Body).Decode(&indexEntries))
	require.Len(t, indexEntries, 1)
	assert.Equal(t, "main", indexEntries[0].Name)

	resDoc, err := http.Get(ts.URL + "/tiles/main.json")
	require.NoError(t, err)
	defer func() { require.NoError(t, resDoc.Body.Close()) }()
	assert.Equal(t, http.StatusOK, resDoc.StatusCode)

	var doc layer.TileJSONDocument
	require.NoError(t, json.NewDecoder(resDoc.Body).Decode(&doc))
	assert.Equal(t, "main", doc.Name)
}

func Test_SetupHandlers_TileJSON_Disabled_RoutesNotRegistered(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(cfg, c, nil, nil)
	require.NoError(t, err)

	ent := &entities.Entities{LayerGroup: lg, Auth: auth}

	rootHandler, err := setupTestRootHandler(&cfg, ent)
	require.NoError(t, err)

	ts := httptest.NewServer(rootHandler)
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(ts.URL + "/tilejson.json")
	require.NoError(t, err)
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.NotEqual(t, http.StatusOK, res.StatusCode, "TileJSON index should not be served when disabled")
}
