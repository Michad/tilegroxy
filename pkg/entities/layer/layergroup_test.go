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

package layer

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
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

func (p *closableProvider) PreAuth(_ context.Context, _ ProviderContext) (ProviderContext, error) {
	return ProviderContext{}, nil
}

func (p *closableProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func Test_LayerGroupClosesProviders(t *testing.T) {
	prov := &closableProvider{}
	lg := &LayerGroup{layers: []*Layer{{Provider: prov}}}

	require.NoError(t, lg.Close(context.Background()))

	// A custom provider whose script defines a close hook is the case this exists for.
	assert.True(t, prov.closed)
}

func Test_LayerGroupCloseIgnoresPlainProviders(t *testing.T) {
	// Most providers hold nothing and must not be required to implement Close.
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

// ProviderWrapper wraps every constructed provider for tracing, so Close has to see through it to
// the real provider or nothing nested inside blend/fallback/ref would ever be released.
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

// An unsanitized layer ID with a space produces an invalid OTEL instrument name, failing
// Int64Counter construction, which is fatal at startup and silent on hot reload.
func Test_ConstructLayerGroup_LayerIDWithSpaceDoesNotFailConstruction(t *testing.T) {
	RegisterProvider(docExampleSampleRegistration{})

	layers := []config.LayerConfig{
		{ID: "my layer", Provider: map[string]any{"name": "doc-example-sample"}},
	}

	lg, err := ConstructLayerGroup(context.Background(), config.Config{Layers: layers}, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, lg)
}

func Test_ConstructLayerGroup_LayerIDWithNonASCIIDoesNotFailConstruction(t *testing.T) {
	RegisterProvider(docExampleSampleRegistration{})

	layers := []config.LayerConfig{
		{ID: "层", Provider: map[string]any{"name": "doc-example-sample"}},
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

	// Can't statically prove "maybe_pattern_match" doesn't match the pattern layer, so no error
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

func twoCacheRegistry(t *testing.T) *cache.CacheRegistry {
	t.Helper()

	cache.RegisterCache(namedStubCacheRegistration{name: "stub-layer-a"})
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-layer-b"})

	reg, err := cache.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
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

	wrapper, ok := c.(cache.CacheWrapper)
	require.True(t, ok)

	return wrapper.Name
}

func Test_ConstructLayerGroup_LayerUsesOverriddenCache(t *testing.T) {
	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "plain", Provider: map[string]any{"name": "doc-example-sample"}},
		{ID: "overridden", Provider: map[string]any{"name": "doc-example-sample"}, Cache: "special"},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, twoCacheRegistry(t), nil, nil)
	require.NoError(t, err)

	assert.Equal(t, "stub-layer-a", cacheName(t, lg.layers[0].Cache))
	assert.Equal(t, "stub-layer-b", cacheName(t, lg.layers[1].Cache))
}

func Test_ConstructLayerGroup_UnknownLayerCacheErrors(t *testing.T) {
	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "bad", Provider: map[string]any{"name": "doc-example-sample"}, Cache: "nonexistent"},
	}}

	_, err := ConstructLayerGroup(context.Background(), cfg, twoCacheRegistry(t), nil, nil)
	require.ErrorContains(t, err, "nonexistent")
}

// Coalescing defaults to on only when the layer actually caches. A layer overriding a noop default
// with a real cache has to pick that up from its own cache, not the group's.
func Test_ConstructLayerGroup_CoalesceFollowsLayerCache(t *testing.T) {
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-layer-real"})

	// The real noop lives in internal/caches, which this package can't import, so a stub stands in
	// under the same name. Coalescing keys off the wrapper name, which is what matters here.
	cache.RegisterCache(namedStubCacheRegistration{name: "none"})

	reg, err := cache.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "off", "name": "none"},
		{"id": "on", "name": "stub-layer-real"},
	}, "", nil, cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "uncached", Provider: map[string]any{"name": "doc-example-sample"}},
		{ID: "cached", Provider: map[string]any{"name": "doc-example-sample"}, Cache: "on"},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, reg, nil, nil)
	require.NoError(t, err)

	assert.False(t, lg.layers[0].allowCoalesce)
	assert.True(t, lg.layers[1].allowCoalesce)
}

// nestingStubCache stands in for the wrapping caches (multi, ttl, tenant) that live in
// internal/caches, which this package can't import. The exported Cache field is what the registry
// walks to find children.
type nestingStubCache struct {
	Cache cache.Cache
}

func (nestingStubCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
func (nestingStubCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error { return nil }
func (nestingStubCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}

type nestingStubCacheRegistration struct {
	name string
}

func (s nestingStubCacheRegistration) Name() string { return s.name }
func (s nestingStubCacheRegistration) InitializeConfig() any {
	return struct{ Cache map[string]interface{} }{}
}

func (s nestingStubCacheRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(struct{ Cache map[string]interface{} })

	inner, err := cache.ConstructCache(config.Cache, deps)
	if err != nil {
		return nil, err
	}

	return nestingStubCache{Cache: inner}, nil
}

// A tenant cache means the provider's output varies by who asked, so coalescing - which hands
// every waiter the leader's tile - must not turn itself on. Regression test for #942.
func Test_ConstructLayerGroup_CoalesceOffForTenantCache(t *testing.T) {
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-coalesce-inner"})
	cache.RegisterCache(nestingStubCacheRegistration{name: "tenant"})
	cache.RegisterCache(nestingStubCacheRegistration{name: "stub-coalesce-outer"})

	reg, err := cache.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "plain", "name": "stub-coalesce-inner"},
		{"id": "tenanted", "name": "tenant", "cache": map[string]interface{}{"name": "stub-coalesce-inner"}},
		{"id": "nested", "name": "stub-coalesce-outer", "cache": map[string]interface{}{
			"name":  "tenant",
			"cache": map[string]interface{}{"name": "stub-coalesce-inner"},
		}},
	}, "", nil, cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	allow := true
	cfg := config.Config{Layers: []config.LayerConfig{
		{ID: "plain", Provider: map[string]any{"name": "doc-example-sample"}, Cache: "plain"},
		{ID: "tenanted", Provider: map[string]any{"name": "doc-example-sample"}, Cache: "tenanted"},
		{ID: "nested", Provider: map[string]any{"name": "doc-example-sample"}, Cache: "nested"},
		{ID: "override", Provider: map[string]any{"name": "doc-example-sample"}, Cache: "tenanted", AllowCoalesce: &allow},
	}}

	lg, err := ConstructLayerGroup(context.Background(), cfg, reg, nil, nil)
	require.NoError(t, err)

	assert.True(t, lg.layers[0].allowCoalesce, "a plain cache should still auto-enable coalescing")
	assert.False(t, lg.layers[1].allowCoalesce, "a tenant cache should auto-disable coalescing")
	assert.False(t, lg.layers[2].allowCoalesce, "a tenant cache nested under another cache should also auto-disable coalescing")
	assert.True(t, lg.layers[3].allowCoalesce, "an explicit allowcoalesce must still win over the tenant default")
}

// A provider that builds its request out of the requester's identity returns different tiles to
// different callers, so coalescing - which hands every waiter the leader's tile - must not turn
// itself on. Regression test for #942.
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

// urlStubProvider stands in for the real providers that interpolate placeholders into a URL; those
// live in internal/providers, which this package can't import.
type urlStubProvider struct{}

func (urlStubProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	return providerContext, nil
}

func (urlStubProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{}, nil
}

func (urlStubProvider) DataType() config.DataType { return config.DataTypeUnknown }

type urlStubProviderRegistration struct{}

func (urlStubProviderRegistration) Name() string { return "stub-url" }
func (urlStubProviderRegistration) InitializeConfig() any {
	return struct{ URL string }{}
}
func (urlStubProviderRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }
func (urlStubProviderRegistration) Initialize(_ any, _ ProviderDeps) (Provider, error) {
	return urlStubProvider{}, nil
}

func Test_ConstructLayerGroup_CoalesceOffForIdentityPlaceholder(t *testing.T) {
	cache.RegisterCache(namedStubCacheRegistration{name: "stub-placeholder"})
	RegisterProvider(urlStubProviderRegistration{})

	reg, err := cache.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
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
