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
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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

func Test_PreviewHandler_ValidLayer_ReturnsHTMLWithTileURL(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/main", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "main")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, res.Header.Get("Content-Type"), "text/html")

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), `tiles\/main\/{z}\/{x}\/{y}`)
	assert.Contains(t, string(body), "maplibre")
}

// A pattern layer's ParamValidator is operator-configured but can be as permissive as ".*", so the
// matched layer name reaching this handler can contain attacker-chosen characters. This verifies
// the tile URL - built from that name - can't break out of the JS string literal it's placed in.
func Test_PreviewHandler_PatternLayerWithHostileName_EscapesIntoTemplate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:             "main",
			Pattern:        "{name}",
			ParamValidator: map[string]string{"name": ".*"},
			Provider:       map[string]interface{}{"name": "static", "color": "FFF"},
		},
	}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	hostileName := `x"});</script><script>alert(1)</script>{L.tileLayer("x`
	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/"+hostileName, nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", hostileName)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	assert.NotContains(t, string(body), "<script>alert(1)</script>")
	assert.NotContains(t, string(body), `x"});</script>`)
}

func Test_PreviewHandler_UnknownLayer_Returns401(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/doesnotexist", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "doesnotexist")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func Test_PreviewHandler_VectorLayer_NotesInBody(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "vec", LayerMetadata: config.LayerMetadata{DataType: config.DataTypeMVT}, Provider: map[string]interface{}{"name": "proxy", "url": "http://example.com/{z}/{x}/{y}"}},
	}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/vec", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "vec")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "vector tile layer")
}

func Test_PreviewHandler_VectorLayer_NameOverride_UsesQueryParam(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "vec", LayerMetadata: config.LayerMetadata{DataType: config.DataTypeMVT}, Provider: map[string]interface{}{"name": "proxy", "url": "http://example.com/{z}/{x}/{y}"}},
	}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/vec?name=custom_layer", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "vec")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "custom_layer")
	assert.Contains(t, string(body), "sourceLayerKnown =  true")
}

func Test_PreviewHandler_LayerWithBounds_UsesFitBounds(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:            "bounded",
			Provider:      map[string]interface{}{"name": "static", "color": "FFF"},
			LayerMetadata: config.LayerMetadata{Bounds: config.BoundsConfig{South: 10, North: 20, West: 30, East: 40}},
		},
	}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/bounded", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "bounded")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "var bounds =")
	assert.Contains(t, string(body), "30")
}

func Test_PreviewHandler_AllowedArea_IntersectsBounds(t *testing.T) {
	cfg := config.DefaultConfig()
	layerCfg := staticLayerConfig("main")
	layerCfg.Bounds = config.BoundsConfig{South: -10, North: 10, West: -10, East: 10}
	cfg.Layers = []config.LayerConfig{layerCfg}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	ctx := pkg.BackgroundContext()
	ctxAllowedArea, _ := pkg.AllowedAreaFromContext(ctx)
	*ctxAllowedArea = pkg.Bounds{South: -5, North: 5, West: -5, East: 20, SRID: pkg.SRIDWGS84}

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/main", nil).WithContext(ctx)
	req.SetPathValue("layer", "main")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	// The intersection of the layer's [-10,10]x[-10,10] bounds with the allowed [-5,5]x[-5,20]
	// area narrows south/north to -5/5, so those should appear in the rendered bounds array.
	assert.Contains(t, string(body), "var bounds =")
	assert.Contains(t, string(body), "-5")
}

func Test_PreviewHandler_ReloadEntities_SwapsGenerationAndReleasesOld(t *testing.T) {
	cfg := config.DefaultConfig()
	oldGen := newGeneration(&cfg, &entities.Entities{})
	newGen := oldGen.succeededBy(&entities.Entities{})

	h := newPreviewHandler(oldGen)

	h.mu.RLock()
	assert.Equal(t, oldGen, h.current)
	h.mu.RUnlock()

	h.reload(newGen)

	h.mu.RLock()
	assert.Equal(t, newGen, h.current)
	h.mu.RUnlock()
}

func Test_PreviewHandler_LayerScope_RestrictsAccess(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main"), staticLayerConfig("other")}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	ctx := pkg.BackgroundContext()
	limitLayers, _ := pkg.LimitLayersFromContext(ctx)
	*limitLayers = true
	allowedLayers, _ := pkg.AllowedLayersFromContext(ctx)
	*allowedLayers = []string{"main"}

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/other", nil).WithContext(ctx)
	req.SetPathValue("layer", "other")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func Test_PreviewHandler_NonGetMethod_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	req := httptest.NewRequest(http.MethodPost, "http://internal-host/preview/main", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "main")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)
}

func Test_PreviewHandler_OptionsMethod_NoContent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	req := httptest.NewRequest(http.MethodOptions, "http://internal-host/preview/main", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "main")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusNoContent, res.StatusCode)
}

func Test_SetupHandlers_Preview_RegisteredWhenNotProduction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Production = false
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(context.Background(), cfg, cache.NewSingleCacheRegistry(c), nil, nil)
	require.NoError(t, err)

	ent := &entities.Entities{LayerGroup: lg, Auth: auth}

	rootHandler, err := setupTestRootHandler(&cfg, ent)
	require.NoError(t, err)

	ts := httptest.NewServer(rootHandler)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/preview/main")
	require.NoError(t, err)
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "maplibre")
}

func Test_SetupHandlers_Preview_NotRegisteredWhenProduction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Production = true
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	var auth authentication.Authentication = authentications.Noop{}
	var c cache.Cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(context.Background(), cfg, cache.NewSingleCacheRegistry(c), nil, nil)
	require.NoError(t, err)

	ent := &entities.Entities{LayerGroup: lg, Auth: auth}

	rootHandler, err := setupTestRootHandler(&cfg, ent)
	require.NoError(t, err)

	ts := httptest.NewServer(rootHandler)
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(ts.URL + "/preview/main")
	require.NoError(t, err)
	defer func() { require.NoError(t, res.Body.Close()) }()

	assert.NotEqual(t, http.StatusOK, res.StatusCode, "preview should not be served when Production is set")
}

func Test_PreviewHandler_IgnoresReloadedServerConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	ent := buildTileJSONTestServing(t, cfg)
	h := newPreviewHandler(ent)

	h.reload(ent.succeededBy(&entities.Entities{LayerGroup: ent.layerGroup(), Auth: ent.auth()}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/preview/main", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "main")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `http:\/\/example.com\/tiles\/main\/{z}\/{x}\/{y}`)
	assert.NotContains(t, string(body), "maps")
}

func Test_PreviewHandler_NoMetadata_ProbesGuessedLayer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "vec", LayerMetadata: config.LayerMetadata{DataType: config.DataTypeMVT}, Provider: map[string]interface{}{"name": "proxy", "url": "http://example.com/{z}/{x}/{y}"}},
	}

	h := newPreviewHandler(buildTileJSONTestServing(t, cfg))

	req := httptest.NewRequest(http.MethodGet, "http://internal-host/preview/vec", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "vec")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `["vec"]`)
	assert.Contains(t, string(body), "sourceLayerKnown =  false")
	assert.Contains(t, string(body), "var bounds = null")
}

func Test_PreviewSourceLayers(t *testing.T) {
	assert.Empty(t, previewSourceLayers(nil))
	assert.Empty(t, previewSourceLayers([]config.VectorLayer{}))
	assert.Equal(t, []string{"roads", "water"}, previewSourceLayers([]config.VectorLayer{{ID: "roads"}, {}, {ID: "water"}}))
}

func Test_PreviewTemplate_CenterWithoutBounds(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, previewPageTemplate.Execute(&buf, previewTemplateData{
		LayerName:          "l",
		Center:             []float64{12.5, 41.9, 6},
		IsVector:           true,
		VectorSourceLayers: []string{"a"},
	}))

	assert.Contains(t, buf.String(), "var center = [12.5,41.9,6]")
	assert.Contains(t, buf.String(), "var bounds = null")
}
