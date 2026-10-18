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

package layers

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type closableProvider struct {
	closed bool
}

func (p *closableProvider) Close(_ context.Context) error {
	p.closed = true
	return nil
}

func (p *closableProvider) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{}, nil
}

func (p *closableProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func Test_LayerGroupClosesProviders(t *testing.T) {
	prov := &closableProvider{}
	lg := &LayerGroup{layers: []*Layer{{Provider: prov}}}

	require.NoError(t, lg.Close(context.Background()))

	// A custom provider whose script defines close is why this exists
	assert.True(t, prov.closed)
}

func Test_LayerGroupCloseIgnoresPlainProviders(t *testing.T) {
	// Most providers hold nothing and mustn't be required to implement Close
	lg := &LayerGroup{layers: []*Layer{{Provider: nil}}}

	require.NoError(t, lg.Close(context.Background()))
}

func Test_LayerGroupCloseHandlesNilLayerGroup(t *testing.T) {
	var lg *LayerGroup

	require.NoError(t, lg.Close(context.Background()))
}

func Test_LayerGroupCloseSkipsNilLayers(t *testing.T) {
	lg := &LayerGroup{layers: []*Layer{nil}}

	require.NoError(t, lg.Close(context.Background()))
}

// Close must see through the tracing wrapper or nothing nested inside blend/fallback/ref is released
func Test_LayerGroupClosesProviderThroughWrapper(t *testing.T) {
	prov := &closableProvider{}
	wrapped := ProviderWrapper{Name: "test", Provider: prov}
	lg := &LayerGroup{layers: []*Layer{{Provider: wrapped}}}

	require.NoError(t, lg.Close(context.Background()))

	assert.True(t, prov.closed)
}

func refProvider(target string) map[string]any {
	return map[string]any{"name": "ref", "layer": target}
}

func staticProvider() map[string]any {
	return map[string]any{"name": "static", "color": "FFF"}
}

// An unsanitized ID with a space fails OTEL instrument construction, fatally at startup and silently on hot reload
func Test_ConstructLayerGroup_LayerIDWithSpaceDoesNotFailConstruction(t *testing.T) {
	layer.RegisterProvider(sampleProviderRegistration{})

	layers := []config.LayerConfig{
		{ID: "my layer", Provider: map[string]any{"name": "sample-provider"}},
	}

	lg, err := ConstructLayerGroup(context.Background(), config.Config{Layers: layers}, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, lg)
}

func Test_ConstructLayerGroup_LayerIDWithNonASCIIDoesNotFailConstruction(t *testing.T) {
	layer.RegisterProvider(sampleProviderRegistration{})

	layers := []config.LayerConfig{
		{ID: "层", Provider: map[string]any{"name": "sample-provider"}},
	}

	lg, err := ConstructLayerGroup(context.Background(), config.Config{Layers: layers}, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, lg)
}

func Test_ConstructLayerGroup_DuplicateLayerIDErrors(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "dupe", Provider: staticProvider()},
		{ID: "dupe", Provider: staticProvider()},
	}

	_, err := ConstructLayerGroup(context.Background(), config.Config{Layers: layers}, nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate layer id")
}

func Test_ValidateRefs_DirectCycle(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("b")},
		{ID: "b", Provider: refProvider("a")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func Test_ValidateRefs_SelfCycle(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("a")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func Test_ValidateRefs_CycleNestedInBlend(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: map[string]any{
			"name": "blend",
			"providers": []any{
				refProvider("b"),
				staticProvider(),
			},
		}},
		{ID: "b", Provider: refProvider("a")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func Test_ValidateRefs_DanglingTarget(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("nonexistent")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown layer")
}

func Test_ValidateRefs_DanglingTargetSkippedWithPatternLayers(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("maybe_pattern_match")},
		{ID: "b", Pattern: "pattern_{x}", Provider: staticProvider()},
	}

	// Can't statically prove "maybe_pattern_match" doesn't match the pattern layer
	err := validateRefs(layers)
	require.NoError(t, err)
}

func Test_ValidateRefs_ValidChain(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("b")},
		{ID: "b", Provider: refProvider("c")},
		{ID: "c", Provider: staticProvider()},
	}

	err := validateRefs(layers)
	require.NoError(t, err)
}

func Test_ValidateRefs_NoRefs(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: staticProvider()},
	}

	err := validateRefs(layers)
	require.NoError(t, err)
}

type namedStubCache struct {
	name string
}

func (namedStubCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
func (namedStubCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error { return nil }
func (namedStubCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

func twoCacheRegistry(t *testing.T) *caches.CacheRegistry {
	t.Helper()

	cache.RegisterCache(namedStubCacheRegistration{name: "stub-layer-a"})
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-layer-b"})

	reg, err := caches.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "main", "name": "stub-layer-a"},
		{"id": "special", "name": "stub-layer-b"},
	}, "", nil, cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	return reg
}

type namedStubCacheRegistration struct {
	name string
}

func (s namedStubCacheRegistration) Name() string          { return s.name }
func (s namedStubCacheRegistration) InitializeConfig() any { return struct{}{} }
func (s namedStubCacheRegistration) Initialize(_ any, _ cache.CacheDeps) (cache.Cache, error) {
	return namedStubCache(s), nil
}

func cacheName(t *testing.T, c cache.Cache) string {
	t.Helper()

	wrapper, ok := c.(caches.CacheWrapper)
	require.True(t, ok)

	return wrapper.Name
}

func Test_ConstructLayerGroup_LayerUsesOverriddenCache(t *testing.T) {
	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "plain", Provider: map[string]any{"name": "sample-provider"}},
		{ID: "overridden", Provider: map[string]any{"name": "sample-provider"}, Cache: "special"},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, twoCacheRegistry(t), nil, nil)
	require.NoError(t, err)

	assert.Equal(t, "stub-layer-a", cacheName(t, lg.layers[0].Cache))
	assert.Equal(t, "stub-layer-b", cacheName(t, lg.layers[1].Cache))
}

func Test_ConstructLayerGroup_UnknownLayerCacheErrors(t *testing.T) {
	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "bad", Provider: map[string]any{"name": "sample-provider"}, Cache: "nonexistent"},
	}}

	_, err := ConstructLayerGroup(context.Background(), cfg, twoCacheRegistry(t), nil, nil)
	require.ErrorContains(t, err, "nonexistent")
}

// A layer overriding a noop default with a real cache must use its own cache to decide
func Test_ConstructLayerGroup_CoalesceFollowsLayerCache(t *testing.T) {
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-layer-real"})

	cache.RegisterCache(noopStubCacheRegistration{})

	reg, err := caches.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "off", "name": "stub-layer-noop"},
		{"id": "on", "name": "stub-layer-real"},
	}, "", nil, cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "uncached", Provider: map[string]any{"name": "sample-provider"}},
		{ID: "cached", Provider: map[string]any{"name": "sample-provider"}, Cache: "on"},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, reg, nil, nil)
	require.NoError(t, err)

	assert.False(t, lg.layers[0].allowCoalesce)
	assert.True(t, lg.layers[1].allowCoalesce)
}

type noopStubCache struct{ namedStubCache }

func (noopStubCache) IsNoop() bool { return true }

type noopStubCacheRegistration struct{}

func (noopStubCacheRegistration) Name() string          { return "stub-layer-noop" }
func (noopStubCacheRegistration) InitializeConfig() any { return struct{}{} }
func (noopStubCacheRegistration) Initialize(_ any, _ cache.CacheDeps) (cache.Cache, error) {
	return noopStubCache{}, nil
}

// A third-party wrapping cache, detected purely through the capability interfaces
type nestingStubCache struct {
	inner cache.Cache
	keyed bool
}

func (c nestingStubCache) Children() []cache.Cache { return []cache.Cache{c.inner} }
func (c nestingStubCache) KeyedByIdentity() bool   { return c.keyed }

func (nestingStubCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
func (nestingStubCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error { return nil }
func (nestingStubCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

type nestingStubCacheRegistration struct {
	name  string
	keyed bool
}

func (s nestingStubCacheRegistration) Name() string { return s.name }
func (s nestingStubCacheRegistration) InitializeConfig() any {
	return struct{ Cache map[string]interface{} }{}
}

func (s nestingStubCacheRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(struct{ Cache map[string]interface{} })

	inner, err := caches.ConstructCache(config.Cache, deps)
	if err != nil {
		return nil, err
	}

	return nestingStubCache{inner: inner, keyed: s.keyed}, nil
}

// Regression test for #942: per-tenant output means coalescing, which shares the leader's tile, must stay off
func Test_ConstructLayerGroup_CoalesceOffForTenantCache(t *testing.T) {
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-coalesce-inner"})
	cache.RegisterCache(nestingStubCacheRegistration{name: "stub-tenant", keyed: true})
	cache.RegisterCache(nestingStubCacheRegistration{name: "stub-coalesce-outer"})

	reg, err := caches.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "plain", "name": "stub-coalesce-inner"},
		{"id": "tenanted", "name": "stub-tenant", "cache": map[string]interface{}{"name": "stub-coalesce-inner"}},
		{"id": "nested", "name": "stub-coalesce-outer", "cache": map[string]interface{}{
			"name":  "stub-tenant",
			"cache": map[string]interface{}{"name": "stub-coalesce-inner"},
		}},
	}, "", nil, cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	allow := true
	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "plain", Provider: map[string]any{"name": "sample-provider"}, Cache: "plain"},
		{ID: "tenanted", Provider: map[string]any{"name": "sample-provider"}, Cache: "tenanted"},
		{ID: "nested", Provider: map[string]any{"name": "sample-provider"}, Cache: "nested"},
		{ID: "override", Provider: map[string]any{"name": "sample-provider"}, Cache: "tenanted", AllowCoalesce: &allow},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, reg, nil, nil)
	require.NoError(t, err)

	assert.True(t, lg.layers[0].allowCoalesce, "a plain cache should still auto-enable coalescing")
	assert.False(t, lg.layers[1].allowCoalesce, "a tenant cache should auto-disable coalescing")
	assert.False(t, lg.layers[2].allowCoalesce, "a tenant cache nested under another cache should also auto-disable coalescing")
	assert.True(t, lg.layers[3].allowCoalesce, "an explicit allowcoalesce must still win over the tenant default")
}

func Test_ConstructLayerGroup_CoalesceOffForBuiltinTenantCache(t *testing.T) {
	reg, err := caches.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "tenanted", "name": "multi", "tiers": []interface{}{
			map[string]interface{}{"name": "memory"},
			map[string]interface{}{"name": "ttl", "ttl": 60, "cache": map[string]interface{}{
				"name":  "tenant",
				"cache": map[string]interface{}{"name": "memory"},
			}},
		}},
		{"id": "noop", "name": "none"},
	}, "", nil, cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "tenanted", Provider: staticProvider(), Cache: "tenanted"},
		{ID: "noop", Provider: staticProvider(), Cache: "noop"},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, reg, nil, nil)
	require.NoError(t, err)

	assert.False(t, lg.layers[0].allowCoalesce)
	assert.True(t, lg.layers[0].CacheControl.PerIdentity)
	assert.False(t, lg.layers[1].allowCoalesce)
	assert.True(t, lg.layers[1].CacheControl.Uncacheable)
}

// Regression test for #942: identity-based requests return different tiles per caller, so coalescing must stay off
func Test_UsesIdentityPlaceholder(t *testing.T) {
	tests := []struct {
		name     string
		provider any
		expected bool
	}{
		{"user in a url", map[string]any{"name": "proxy", "url": "https://example.com/{z}/{x}/{y}?u={ctx.user}"}, true},
		{"tenant in a url", map[string]any{"name": "proxy", "url": "https://example.com/{z}/{x}/{y}?t={ctx.tenant}"}, true},
		{"nested inside another provider", map[string]any{
			"name":    "fallback",
			"primary": map[string]any{"name": "proxy", "url": "https://example.com/{ctx.tenant}/{z}/{x}/{y}"},
		}, true},
		{"inside a list of providers", map[string]any{
			"name":      "blend",
			"providers": []any{map[string]any{"name": "proxy", "url": "https://example.com/{ctx.user}"}},
		}, true},
		{"in a map key", map[string]any{"headers": map[string]any{"{ctx.user}": "x"}}, true},
		{"a different ctx value", map[string]any{"name": "proxy", "url": "https://example.com/{z}/{x}/{y}?a={ctx.User-Agent}"}, false},
		{"no placeholders at all", map[string]any{"name": "proxy", "url": "https://example.com/{z}/{x}/{y}"}, false},
		{"no provider", nil, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, usesIdentityPlaceholder(test.provider))
		})
	}
}

// The real URL-interpolating providers live in internal/providers, which this package can't import
type urlStubProvider struct{}

func (urlStubProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return providerContext, nil
}

func (urlStubProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{}, nil
}

func (urlStubProvider) DataType() config.DataType { return config.DataTypeUnknown }

type urlStubProviderRegistration struct{}

func (urlStubProviderRegistration) Name() string { return "stub-url" }
func (urlStubProviderRegistration) InitializeConfig() any {
	return struct{ URL string }{}
}
func (urlStubProviderRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }
func (urlStubProviderRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return urlStubProvider{}, nil
}

func Test_ConstructLayerGroup_CoalesceOffForIdentityPlaceholder(t *testing.T) {
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-placeholder"})
	layer.RegisterProvider(urlStubProviderRegistration{})

	reg, err := caches.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "real", "name": "stub-placeholder"},
	}, "real", nil, cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	allow := true
	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "plain", Provider: map[string]any{"name": "stub-url", "url": "https://example.com/{z}/{x}/{y}"}},
		{ID: "peruser", Provider: map[string]any{"name": "stub-url", "url": "https://example.com/{z}/{x}/{y}?u={ctx.user}"}},
		{ID: "override", Provider: map[string]any{"name": "stub-url", "url": "https://example.com/{z}/{x}/{y}?u={ctx.user}"}, AllowCoalesce: &allow},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, reg, nil, nil)
	require.NoError(t, err)

	assert.True(t, lg.layers[0].allowCoalesce, "a provider with no identity placeholder should still auto-enable coalescing")
	assert.False(t, lg.layers[1].allowCoalesce, "a provider interpolating the user should auto-disable coalescing")
	assert.True(t, lg.layers[2].allowCoalesce, "an explicit allowcoalesce must still win")
}
