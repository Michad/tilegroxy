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

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/stretchr/testify/assert"
)

func Test_TileHandler_AllowedArea(t *testing.T) {
	cfg := config.DefaultConfig()
	mainProvider := make(map[string]interface{})
	mainProvider["name"] = "static"
	mainProvider["color"] = "FFF"
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "main", Provider: mainProvider})
	var auth authentication.Authentication
	var cache caches.Cache
	auth = authentication.Noop{}
	cache = caches.Noop{}
	lg, err := layers.ConstructLayerGroup(cfg, cfg.Layers, &cache)
	assert.NoError(t, err)

	handler := tileHandler{defaultHandler{config: &cfg, auth: &auth, layerGroup: lg}}

	ctx := internal.BackgroundContext()
	b, _ := internal.TileRequest{LayerName: "l", Z: 10, X: 12, Y: 12}.GetBounds()
	ctx.AllowedArea = *b
	req1 := httptest.NewRequest("GET", "http://example.com/tiles/main/10/10/10", nil).WithContext(ctx)
	req1.SetPathValue("layer", "main")
	req1.SetPathValue("z", "10")
	req1.SetPathValue("x", "10")
	req1.SetPathValue("y", "10")

	w1 := httptest.NewRecorder()

	handler.ServeHTTP(w1, req1)

	assert.Equal(t, 401, w1.Result().StatusCode)

	req2 := httptest.NewRequest("GET", "http://example.com/tiles/main/10/12/12", nil).WithContext(ctx)
	req2.SetPathValue("layer", "main")
	req2.SetPathValue("z", "10")
	req2.SetPathValue("x", "12")
	req2.SetPathValue("y", "12")

	w2 := httptest.NewRecorder()

	handler.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Result().StatusCode)
}

func Test_TileHandler_Proxy(t *testing.T) {
	var query string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(200)
		b, _ := images.GetStaticImage("color:FFF")
		w.Write(*b)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.Client.ContentTypes = append(cfg.Client.ContentTypes, "text/html; charset=UTF-8")
	cfg.Client.UnknownLength = true
	mainProvider := make(map[string]interface{})
	mainProvider["name"] = "proxy"
	os.Setenv("TEST", "t")
	mainProvider["url"] = ts.URL + "?a={layer.a}&b={ctx.user}&t={env.TEST}&z={z}&y={y}&x={x}"
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "main", Pattern: "main_{a}", Provider: mainProvider, Client: &cfg.Client})
	var auth authentication.Authentication
	var cache caches.Cache
	auth = authentication.Noop{}
	cache = caches.Noop{}
	lg, err := layers.ConstructLayerGroup(cfg, cfg.Layers, &cache)
	assert.NoError(t, err)

	handler := tileHandler{defaultHandler{config: &cfg, auth: &auth, layerGroup: lg}}

	ctx := internal.BackgroundContext()
	ctx.UserIdentifier = "hi"
	req1 := httptest.NewRequest("GET", "http://example.com/tiles/main/10/10/10", nil).WithContext(ctx)
	req1.SetPathValue("layer", "main_test")
	req1.SetPathValue("z", "10")
	req1.SetPathValue("x", "10")
	req1.SetPathValue("y", "10")

	w1 := httptest.NewRecorder()

	handler.ServeHTTP(w1, req1)

	assert.Equal(t, 200, w1.Result().StatusCode)
	assert.Equal(t, "a=test&b=hi&t=t&z=10&y=10&x=10", query)
}

func Test_TileHandler_RefToStatic(t *testing.T) {
	cfg := config.DefaultConfig()
	mainProvider := make(map[string]interface{})
	mainProvider["name"] = "static"
	mainProvider["color"] = "FFF"
	refProvider := make(map[string]interface{})
	refProvider["name"] = "ref"
	refProvider["layer"] = "main_white"
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "main", Pattern: "main_{something}", Provider: mainProvider})
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "ref", Pattern: "test", Provider: refProvider})
	var auth authentication.Authentication
	var cache caches.Cache
	auth = authentication.Noop{}
	cache = caches.Noop{}
	lg, err := layers.ConstructLayerGroup(cfg, cfg.Layers, &cache)
	assert.NoError(t, err)

	handler := tileHandler{defaultHandler{config: &cfg, auth: &auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://example.com/tiles/test/10/10/10", nil).WithContext(internal.BackgroundContext())
	req1.SetPathValue("layer", "test")
	req1.SetPathValue("z", "10")
	req1.SetPathValue("x", "10")
	req1.SetPathValue("y", "10")

	w1 := httptest.NewRecorder()

	handler.ServeHTTP(w1, req1)
	res1 := w1.Result()

	assert.Equal(t, 200, res1.StatusCode)
	b1, err := io.ReadAll(res1.Body)
	assert.NoError(t, err)

	req2 := httptest.NewRequest("GET", "http://example.com/tiles/main_a/10/10/10", nil).WithContext(internal.BackgroundContext())
	req2.SetPathValue("layer", "main_a")
	req2.SetPathValue("z", "10")
	req2.SetPathValue("x", "10")
	req2.SetPathValue("y", "10")

	w2 := httptest.NewRecorder()

	handler.ServeHTTP(w2, req2)

	res2 := w2.Result()

	assert.Equal(t, 200, res2.StatusCode)
	b2, err := io.ReadAll(res2.Body)
	assert.NoError(t, err)

	assert.Equal(t, b2, b1)

	img, _ := images.GetStaticImage("color:FFF")
	assert.Equal(t, *img, b1)
}
