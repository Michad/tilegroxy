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
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
)

// testServing builds what a handler serves the way startup does, wrapping the entities in a
// generation so the handlers reach them by the same path they do in production.
func testServing(cfg *config.Config, auth authentication.Authentication, lg *layer.LayerGroup) *generation {
	return testServingWithAnalytics(cfg, auth, lg, nil)
}

func testServingWithAnalytics(cfg *config.Config, auth authentication.Authentication, lg *layer.LayerGroup, a *analytics.AnalyticsWrapper) *generation {
	return newGeneration(cfg, &entities.Entities{LayerGroup: lg, Auth: auth, Analytics: a})
}

func Test_SucceededBy_KeepsNonReloadableValues(t *testing.T) {
	startCfg := config.DefaultConfig()
	startCfg.Server.RootPath = "/start/"
	startCfg.Server.TilePath = "tiles"
	startCfg.Error.Mode = config.ModeErrorImageHeader
	start := newGeneration(&startCfg, nil)

	next := start.succeededBy(&entities.Entities{})

	assert.Equal(t, start.serverCfg, next.serverCfg, "server is not reloadable")
	assert.Equal(t, start.errCfg, next.errCfg, "error is not reloadable")
	assert.Equal(t, "/start/tiles", next.tilePathPrefix(), "the advertised tile path must match the route registered at startup")
}

func Test_SucceededBy_AppliesNewEntities(t *testing.T) {
	startCfg := config.DefaultConfig()
	start := newGeneration(&startCfg, nil)

	lg := &layer.LayerGroup{}
	next := start.succeededBy(&entities.Entities{LayerGroup: lg})

	assert.Same(t, lg, next.layerGroup())
}

// The handlers must not be able to reach the live config, since anything reachable from a
// request is something a reload can change. The pinned sections are held by value for that
// reason; a pointer to any of them would alias whatever the reload built.
func Test_Generation_HoldsNoConfigPointer(t *testing.T) {
	banned := []reflect.Type{
		reflect.TypeOf(&config.Config{}),
		reflect.TypeOf(&config.ServerConfig{}),
		reflect.TypeOf(&config.ErrorConfig{}),
	}

	structType := reflect.TypeOf(generation{})

	for i := range structType.NumField() {
		field := structType.Field(i)

		assert.NotContains(t, banned, field.Type,
			"generation."+field.Name+" points into a config a reload can replace, which makes non-reloadable values reloadable")
	}
}

// The Entities set is the sole owner of the entities. A generation field caching one of them
// alongside it could go stale against the set a request is actually reading through.
func Test_Generation_CachesNoEntity(t *testing.T) {
	structType := reflect.TypeOf(generation{})

	entityFields := reflect.TypeOf(entities.Entities{})

	for i := range structType.NumField() {
		field := structType.Field(i)

		for j := range entityFields.NumField() {
			assert.NotEqual(t, entityFields.Field(j).Type, field.Type,
				"generation."+field.Name+" duplicates an entity the entity set already owns; read it through that instead")
		}
	}
}

func Test_DefaultHandler_RedirectUsesStartupPaths(t *testing.T) {
	startCfg := config.DefaultConfig()
	startCfg.Server.RootPath = "/"
	startCfg.Server.DocsPath = "docs"

	h := defaultHandler{newGeneration(&startCfg, nil)}
	h.generation = h.succeededBy(nil)

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

	ent := newGeneration(&startCfg, nil)
	ent = ent.succeededBy(&entities.Entities{})

	w := httptest.NewRecorder()
	ent.writeHeaders(w)

	assert.Equal(t, "yes", w.Header().Get("X-Old"))
	assert.NotEmpty(t, w.Header().Get("X-Powered-By"), "production is not reloadable")
}

// No generation installed must read as empty rather than panicking, since the accessors run
// before startup finishes wiring one in.
func Test_Generation_Nil(t *testing.T) {
	var r *generation

	assert.Nil(t, r.entities())
	assert.Nil(t, r.layerGroup())
	assert.Nil(t, r.auth())
	assert.Nil(t, r.analytics())
}

// A generation holding no entities is likewise empty rather than a panic.
func Test_Generation_WithoutEntities(t *testing.T) {
	r := newGeneration(nil, nil)

	assert.Nil(t, r.entities())
	assert.Nil(t, r.layerGroup())
	assert.Nil(t, r.auth())
	assert.Nil(t, r.analytics())
}

func Test_NewGeneration_NilConfig(t *testing.T) {
	r := newGeneration(nil, nil)

	assert.Equal(t, config.ServerConfig{}, r.serverCfg)
	assert.Equal(t, config.ErrorConfig{}, r.errCfg)
}
