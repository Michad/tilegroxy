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

package caches

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The nesting caches live in this package, so the registry's walk over them is exercised here
// rather than beside the registry itself.
func nestedTestDeps() cache.CacheDeps {
	return cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages}
}

type countingCache struct {
	closed *int
}

func (countingCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
func (countingCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error { return nil }
func (countingCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}
func (c countingCache) Close(_ context.Context) error {
	*c.closed++
	return nil
}

type countingRegistration struct {
	closed *int
}

func (countingRegistration) Name() string          { return "counting-close" }
func (countingRegistration) InitializeConfig() any { return struct{}{} }
func (s countingRegistration) Initialize(_ any, _ cache.CacheDeps) (cache.Cache, error) {
	return countingCache(s), nil
}

// Mirrors the arrangement in the cache configuration docs: a tenant cache wrapping a multi cache,
// where a layer can reference the multi cache nested two levels down.
func Test_Registry_ReferencesNestedCacheByID(t *testing.T) {
	reg, err := cache.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{
			"id":   "tenanted",
			"name": "tenant",
			"cache": map[string]interface{}{
				"id":   "multi",
				"name": "multi",
				"tiers": []interface{}{
					map[string]interface{}{"id": "near", "name": "memory"},
					map[string]interface{}{"id": "far", "name": "none"},
				},
			},
		},
		{"name": "none"},
	}, "multi", nil, nestedTestDeps())
	require.NoError(t, err)

	for _, id := range []string{"tenanted", "multi", "near", "far", "none"} {
		_, ok := reg.Get(id)
		assert.True(t, ok, "expected cache %v to be referenceable", id)
	}

	assert.Equal(t, "multi", reg.DefaultID())

	require.NoError(t, reg.Close(context.Background()))
}

func Test_Registry_NestedIDCollidesWithTopLevel(t *testing.T) {
	_, err := cache.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{
			"id":   "outer",
			"name": "ttl",
			"ttl":  60,
			"cache": map[string]interface{}{
				"id":   "outer",
				"name": "none",
			},
		},
	}, "", nil, nestedTestDeps())

	require.ErrorContains(t, err, "outer")
}

// A nested cache is closed by the parent that constructed it, so surfacing it in the registry must
// not cause a second close.
func Test_Registry_ClosesNestedCacheOnce(t *testing.T) {
	closed := 0
	cache.RegisterCache(countingRegistration{closed: &closed})

	reg, err := cache.ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{
			"id":   "outer",
			"name": "multi",
			"tiers": []interface{}{
				map[string]interface{}{"id": "inner", "name": "counting-close"},
			},
		},
	}, "", nil, nestedTestDeps())
	require.NoError(t, err)

	_, ok := reg.Get("inner")
	require.True(t, ok)

	require.NoError(t, reg.Close(context.Background()))
	assert.Equal(t, 1, closed)
}

// ContainsCache has to find a tenant cache wherever it sits, since that's what decides whether a
// layer coalesces concurrent requests by default. Exercised against the real nesting caches.
func Test_ContainsCache_FindsTenantAtAnyDepth(t *testing.T) {
	deps := nestedTestDeps()

	plain, err := cache.ConstructCache(map[string]interface{}{"name": "memory"}, deps)
	require.NoError(t, err)
	assert.False(t, cache.ContainsCache(plain, "tenant"))

	direct, err := cache.ConstructCache(map[string]interface{}{
		"name":  "tenant",
		"cache": map[string]interface{}{"name": "memory"},
	}, deps)
	require.NoError(t, err)
	assert.True(t, cache.ContainsCache(direct, "tenant"))

	underTTL, err := cache.ConstructCache(map[string]interface{}{
		"name": "ttl",
		"ttl":  60,
		"cache": map[string]interface{}{
			"name":  "tenant",
			"cache": map[string]interface{}{"name": "memory"},
		},
	}, deps)
	require.NoError(t, err)
	assert.True(t, cache.ContainsCache(underTTL, "tenant"))

	inOneTier, err := cache.ConstructCache(map[string]interface{}{
		"name": "multi",
		"tiers": []map[string]interface{}{
			{"name": "memory"},
			{"name": "tenant", "cache": map[string]interface{}{"name": "memory"}},
		},
	}, deps)
	require.NoError(t, err)
	assert.True(t, cache.ContainsCache(inOneTier, "tenant"))

	noTenant, err := cache.ConstructCache(map[string]interface{}{
		"name": "multi",
		"tiers": []map[string]interface{}{
			{"name": "memory"},
			{"name": "ttl", "ttl": 60, "cache": map[string]interface{}{"name": "memory"}},
		},
	}, deps)
	require.NoError(t, err)
	assert.False(t, cache.ContainsCache(noTenant, "tenant"))
}
