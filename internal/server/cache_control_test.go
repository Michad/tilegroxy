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
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cacheControlNow = time.Unix(1700000000, 0)

func layerWithFacts(facts layer.CacheControlFacts, cfg *config.CacheControlConfig) *layer.Layer {
	return &layer.Layer{
		ID:           "test",
		Config:       config.LayerConfig{ID: "test", CacheControl: cfg},
		CacheControl: facts,
	}
}

// A tile generated the given number of seconds before the reference time
func tileAged(seconds int) *pkg.Image {
	return &pkg.Image{CreatedAt: cacheControlNow.Add(-time.Duration(seconds) * time.Second).Unix()}
}

func Test_CacheControl_DisabledReturnsNothing(t *testing.T) {
	cfg := config.DefaultConfig().Server.CacheControl
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Empty(t, generateCacheControlValue(cfg, l, tileAged(0), cacheControlNow))
}

// The documented example: a one hour TTL, a tile generated 50 minutes ago
func Test_CacheControl_DerivesRemainingLifetime(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "public, max-age=600", generateCacheControlValue(cfg, l, tileAged(3000), cacheControlNow))
}

func Test_CacheControl_ExpiredTileFloorsAtZero(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "public, max-age=0", generateCacheControlValue(cfg, l, tileAged(7200), cacheControlNow))
}

// An entry written by a path that bypasses the TTL cache is treated as fresh, not as 1970
func Test_CacheControl_UnknownAgeGetsFullTTL(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "public, max-age=3600", generateCacheControlValue(cfg, l, &pkg.Image{}, cacheControlNow))
}

func Test_CacheControl_NoTTLOmitsMaxAge(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{}, nil)

	assert.Equal(t, "public", generateCacheControlValue(cfg, l, tileAged(10), cacheControlNow))
}

func Test_CacheControl_UncacheableLayerReturnsNoStore(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{Uncacheable: true}, nil)

	assert.Equal(t, "no-store", generateCacheControlValue(cfg, l, tileAged(10), cacheControlNow))
}

func Test_CacheControl_PerIdentityLayerIsPrivate(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{PerIdentity: true, TTL: time.Hour}, nil)

	assert.Equal(t, "private, max-age=3000", generateCacheControlValue(cfg, l, tileAged(600), cacheControlNow))
}

func Test_CacheControl_UndeterminedLayerStillHonorsServerBlock(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), MaxAge: new(uint(300))}

	assert.Equal(t, "public, max-age=300", generateCacheControlValue(cfg, nil, tileAged(10), cacheControlNow))
}

// The documented example adding directives that are never derived
func Test_CacheControl_AddsSharedMaxAgeAndStaleWindow(t *testing.T) {
	cfg := config.CacheControlConfig{
		Enabled:              new(true),
		SharedMaxAge:         new(uint(86400)),
		StaleWhileRevalidate: new(uint(60)),
	}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "public, max-age=600, s-maxage=86400, stale-while-revalidate=60", generateCacheControlValue(cfg, l, tileAged(3000), cacheControlNow))
}

func Test_CacheControl_ExplicitMaxAgeReplacesDerivedLifetime(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), MaxAge: new(uint(86400))}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "public, max-age=86400", generateCacheControlValue(cfg, l, tileAged(3000), cacheControlNow))
}

// Zero is a set value, not an unset one, so it revalidates every time rather than deriving
func Test_CacheControl_ZeroMaxAgeIsDistinctFromUnset(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), MaxAge: new(uint(0))}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "public, max-age=0", generateCacheControlValue(cfg, l, tileAged(10), cacheControlNow))
}

// The documented example of opting out of derivation entirely
func Test_CacheControl_AutoFalseReturnsOnlyWhatIsConfigured(t *testing.T) {
	cfg := config.CacheControlConfig{
		Enabled:    new(true),
		Auto:       new(false),
		Visibility: config.CacheVisibilityPublic,
		MaxAge:     new(uint(86400)),
		Extra:      []string{"immutable"},
	}
	l := layerWithFacts(layer.CacheControlFacts{Uncacheable: true, PerIdentity: true, TTL: time.Minute}, nil)

	assert.Equal(t, "public, max-age=86400, immutable", generateCacheControlValue(cfg, l, tileAged(10), cacheControlNow))
}

func Test_CacheControl_AutoFalseWithNothingElseReturnsNothing(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), Auto: new(false)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Empty(t, generateCacheControlValue(cfg, l, tileAged(10), cacheControlNow))
}

func Test_CacheControl_ExplicitNoStoreOverridesDerivation(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), NoStore: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "no-store", generateCacheControlValue(cfg, l, tileAged(10), cacheControlNow))
}

func Test_CacheControl_ExplicitNoStoreFalseKeepsUncacheableLayerStorable(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), NoStore: new(false)}
	l := layerWithFacts(layer.CacheControlFacts{Uncacheable: true}, nil)

	assert.Equal(t, "public", generateCacheControlValue(cfg, l, tileAged(10), cacheControlNow))
}

func Test_CacheControl_ExtraAppendedToDerivedDirectives(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), Extra: []string{"immutable", "stale-if-error=86400"}}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, nil)

	assert.Equal(t, "public, max-age=3600, immutable, stale-if-error=86400", generateCacheControlValue(cfg, l, &pkg.Image{}, cacheControlNow))
}

// The documented example of a layer overriding one directive and keeping the rest
func Test_CacheControl_LayerOverridesFieldByField(t *testing.T) {
	serverCfg := config.CacheControlConfig{Enabled: new(true), SharedMaxAge: new(uint(86400))}
	l := layerWithFacts(
		layer.CacheControlFacts{TTL: time.Hour},
		&config.CacheControlConfig{Visibility: config.CacheVisibilityPrivate},
	)

	merged := mergeCacheControl(serverCfg, l)

	assert.Equal(t, "private, max-age=600, s-maxage=86400", generateCacheControlValue(merged, l, tileAged(3000), cacheControlNow))
}

func Test_CacheControl_LayerWithoutBlockUsesServerBlock(t *testing.T) {
	serverCfg := config.CacheControlConfig{Enabled: new(true), MaxAge: new(uint(60))}
	l := layerWithFacts(layer.CacheControlFacts{}, nil)

	assert.Equal(t, serverCfg, mergeCacheControl(serverCfg, l))
}

// A layer can turn the header on for itself while the server leaves it off
func Test_CacheControl_LayerCanEnableWithoutServer(t *testing.T) {
	serverCfg := config.CacheControlConfig{Enabled: new(false)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, &config.CacheControlConfig{Enabled: new(true)})

	assert.Equal(t, "public, max-age=3600", generateCacheControlValue(mergeCacheControl(serverCfg, l), l, &pkg.Image{}, cacheControlNow))
}

// A layer can turn the header off for itself while the server leaves it on
func Test_CacheControl_LayerCanDisableWhatServerEnabled(t *testing.T) {
	serverCfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, &config.CacheControlConfig{Enabled: new(false)})

	assert.Empty(t, generateCacheControlValue(mergeCacheControl(serverCfg, l), l, &pkg.Image{}, cacheControlNow))
}

// A layer block that says nothing about Enabled still inherits the server's
func Test_CacheControl_LayerWithoutEnabledInheritsServer(t *testing.T) {
	serverCfg := config.CacheControlConfig{Enabled: new(true)}
	l := layerWithFacts(layer.CacheControlFacts{TTL: time.Hour}, &config.CacheControlConfig{Visibility: config.CacheVisibilityPrivate})

	assert.Equal(t, "private, max-age=3600", generateCacheControlValue(mergeCacheControl(serverCfg, l), l, &pkg.Image{}, cacheControlNow))
}

func Test_CacheControl_ValidateAcceptsDisabledNonsense(t *testing.T) {
	cfg := config.CacheControlConfig{Visibility: "sideways"}

	require.NoError(t, ValidateCacheControl(cfg, "server.cachecontrol", config.DefaultConfig().Error.Messages))
}

func Test_CacheControl_ValidateRejectsUnknownVisibility(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), Visibility: "sideways"}

	err := ValidateCacheControl(cfg, "server.cachecontrol", config.DefaultConfig().Error.Messages)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.cachecontrol.visibility")
}

func Test_CacheControl_ValidateRejectsCommaInExtra(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), Extra: []string{"immutable, no-transform"}}

	err := ValidateCacheControl(cfg, "server.cachecontrol", config.DefaultConfig().Error.Messages)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.cachecontrol.extra")
}

func Test_CacheControl_ValidateRejectsNoStoreWithOtherFields(t *testing.T) {
	for name, cfg := range map[string]config.CacheControlConfig{
		"maxage":               {Enabled: new(true), NoStore: new(true), MaxAge: new(uint(60))},
		"sharedmaxage":         {Enabled: new(true), NoStore: new(true), SharedMaxAge: new(uint(60))},
		"stalewhilerevalidate": {Enabled: new(true), NoStore: new(true), StaleWhileRevalidate: new(uint(60))},
		"visibility":           {Enabled: new(true), NoStore: new(true), Visibility: config.CacheVisibilityPublic},
		"extra":                {Enabled: new(true), NoStore: new(true), Extra: []string{"immutable"}},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateCacheControl(cfg, "server.cachecontrol", config.DefaultConfig().Error.Messages)

			require.Error(t, err)
			assert.Contains(t, err.Error(), name)
		})
	}
}

func Test_CacheControl_ValidateAllowsNoStoreAlone(t *testing.T) {
	cfg := config.CacheControlConfig{Enabled: new(true), NoStore: new(true)}

	require.NoError(t, ValidateCacheControl(cfg, "server.cachecontrol", config.DefaultConfig().Error.Messages))
}

// A layer's block is validated merged with the server's, so the conflict is caught across the two
func Test_CacheControl_ValidateAllCatchesMergedLayerConflict(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.CacheControl = config.CacheControlConfig{Enabled: new(true), MaxAge: new(uint(60))}
	cfg.Layers = []config.LayerConfig{{ID: "l", CacheControl: &config.CacheControlConfig{NoStore: new(true)}}}

	err := validateAllCacheControl(&cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "layer.l.cachecontrol")
}

func cacheControlHandlerRequest(t *testing.T, cfg config.Config, ifNoneMatch string) *http.Response {
	t.Helper()

	lg, auth, err := configToEntities(cfg)
	require.NoError(t, err)

	handler, err := newTileHandler(testServing(&cfg, auth, lg))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/main/10/10/10", nil).WithContext(pkg.BackgroundContext())
	req.SetPathValue("layer", "main")
	req.SetPathValue("z", "10")
	req.SetPathValue("x", "10")
	req.SetPathValue("y", "10")

	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	return w.Result()
}

func cacheControlTestConfig(t *testing.T) config.Config {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Server.CacheControl = config.CacheControlConfig{Enabled: new(true)}
	cfg.Cache = map[string]any{"name": "ttl", "ttl": 3600, "cache": map[string]any{"name": "memory"}}
	cfg.Layers = append(cfg.Layers, config.LayerConfig{ID: "main", Provider: map[string]any{"name": "static", "color": "FFF"}})

	return cfg
}

func Test_CacheControl_HandlerReturnsHeaderOnTile(t *testing.T) {
	r := cacheControlHandlerRequest(t, cacheControlTestConfig(t), "")
	defer func() { require.NoError(t, r.Body.Close()) }()

	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Equal(t, "public, max-age=3600", r.Header.Get("Cache-Control"))
}

func Test_CacheControl_HandlerOmitsHeaderWhenDisabled(t *testing.T) {
	cfg := cacheControlTestConfig(t)
	cfg.Server.CacheControl = config.CacheControlConfig{}

	r := cacheControlHandlerRequest(t, cfg, "")
	defer func() { require.NoError(t, r.Body.Close()) }()

	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Empty(t, r.Header.Get("Cache-Control"))
}

// A 304 saves the body but still has to tell the browser how long it may hold the tile
func Test_CacheControl_HandlerReturnsHeaderOnNotModified(t *testing.T) {
	first := cacheControlHandlerRequest(t, cacheControlTestConfig(t), "")
	require.NoError(t, first.Body.Close())
	etag := first.Header.Get("ETag")
	require.NotEmpty(t, etag)

	r := cacheControlHandlerRequest(t, cacheControlTestConfig(t), etag)
	defer func() { require.NoError(t, r.Body.Close()) }()

	require.Equal(t, http.StatusNotModified, r.StatusCode)
	assert.Equal(t, "public, max-age=3600", r.Header.Get("Cache-Control"))
}

func Test_CacheControl_HandlerReturnsNoStoreOnError(t *testing.T) {
	cfg := cacheControlTestConfig(t)
	cfg.Layers[0].MaxZoom = new(1)

	r := cacheControlHandlerRequest(t, cfg, "")
	defer func() { require.NoError(t, r.Body.Close()) }()

	require.NotEqual(t, http.StatusOK, r.StatusCode)
	assert.Equal(t, noStoreDirective, r.Header.Get("Cache-Control"))
}
