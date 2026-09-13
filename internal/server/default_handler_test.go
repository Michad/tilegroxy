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
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
)

// testEntities builds the handler projection the way startup does, without the entity set a
// generation would carry.
func testEntities(cfg *config.Config, auth authentication.Authentication, lg *layer.LayerGroup) reloadableEntities {
	r := newReloadableEntities(cfg, nil, nil)
	r.auth = auth
	r.layerGroup = lg

	return r
}

func testEntitiesWithAnalytics(cfg *config.Config, auth authentication.Authentication, lg *layer.LayerGroup, a *analytics.AnalyticsWrapper) reloadableEntities {
	r := testEntities(cfg, auth, lg)
	r.analytics = a

	return r
}

// nonReloadableConfig returns a config whose every non-reloadable handler-visible value differs
// from the default, so a reload onto it would show up in any value that leaked through.
func nonReloadableConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Server.RootPath = "/new/"
	cfg.Server.TilePath = "maps"
	cfg.Server.DocsPath = "manual"
	cfg.Server.Headers = map[string]string{"X-New": "yes"}
	cfg.Server.Production = true
	cfg.Server.TileJSON.BaseURLs = []string{"https://new.example.com"}
	cfg.Error.Mode = config.ModeErrorPlainText
	cfg.Error.AlwaysOK = true

	return &cfg
}

func Test_ReloadedFrom_KeepsNonReloadableValues(t *testing.T) {
	startCfg := config.DefaultConfig()
	start := newReloadableEntities(&startCfg, nil, nil)

	next := start.reloadedFrom(nonReloadableConfig(), nil, nil)

	assert.Equal(t, start.serverCfg, next.serverCfg, "server is not reloadable")
	assert.Equal(t, start.errCfg.Mode, next.errCfg.Mode, "error.mode is not reloadable")
	assert.Equal(t, start.errCfg.AlwaysOK, next.errCfg.AlwaysOK, "error.alwaysok is not reloadable")
}

func Test_ReloadedFrom_AppliesReloadableValues(t *testing.T) {
	startCfg := config.DefaultConfig()
	start := newReloadableEntities(&startCfg, nil, nil)

	newCfg := nonReloadableConfig()
	newCfg.Error.Messages.NotAuthorized = "nope"
	newCfg.Error.Images.Other = "other.png"

	next := start.reloadedFrom(newCfg, nil, nil)

	assert.Equal(t, "nope", next.errCfg.Messages.NotAuthorized)
	assert.Equal(t, "other.png", next.errCfg.Images.Other)
}

// The handlers must not be able to reach the live config, since anything reachable from a
// request is something a reload can change. The pinned sections are held by value for that
// reason; a pointer to any of them would alias whatever the reload built.
func Test_ReloadableEntities_HoldsNoConfigPointer(t *testing.T) {
	banned := []reflect.Type{
		reflect.TypeOf(&config.Config{}),
		reflect.TypeOf(&config.ServerConfig{}),
		reflect.TypeOf(&config.ErrorConfig{}),
	}

	structType := reflect.TypeOf(reloadableEntities{})

	for i := range structType.NumField() {
		field := structType.Field(i)

		assert.NotContains(t, banned, field.Type,
			"reloadableEntities."+field.Name+" points into a config a reload can replace, which makes non-reloadable values reloadable")
	}
}

func Test_DefaultHandler_RedirectUsesStartupPaths(t *testing.T) {
	startCfg := config.DefaultConfig()
	startCfg.Server.RootPath = "/"
	startCfg.Server.DocsPath = "docs"

	h := defaultHandler{newReloadableEntities(&startCfg, nil, nil)}
	h.reloadableEntities = h.reloadedFrom(nonReloadableConfig(), nil, nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/", nil))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	assert.Equal(t, "/docs", res.Header.Get("Location"))
}

func Test_WriteHeaders_UsesStartupServerConfig(t *testing.T) {
	startCfg := config.DefaultConfig()
	startCfg.Server.Headers = map[string]string{"X-Old": "yes"}
	startCfg.Server.Production = false

	ent := newReloadableEntities(&startCfg, nil, nil)
	ent = ent.reloadedFrom(nonReloadableConfig(), nil, nil)

	w := httptest.NewRecorder()
	ent.writeHeaders(w)

	assert.Equal(t, "yes", w.Header().Get("X-Old"))
	assert.Empty(t, w.Header().Get("X-New"))
	assert.NotEmpty(t, w.Header().Get("X-Powered-By"), "production is not reloadable")
}

func Test_NewReloadableEntities_NilConfig(t *testing.T) {
	r := newReloadableEntities(nil, nil, nil)

	assert.Equal(t, config.ServerConfig{}, r.serverCfg)
	assert.Equal(t, config.ErrorConfig{}, r.errCfg)
}
