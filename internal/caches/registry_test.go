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
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type closableCache struct {
	closed *int
}

func (closableCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
func (closableCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error { return nil }
func (closableCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error) {
	return false, nil
}
func (c closableCache) Close(_ context.Context) error {
	*c.closed++
	return nil
}

type closableCacheRegistration struct {
	closed *int
}

func (closableCacheRegistration) Name() string          { return "stub-closable" }
func (closableCacheRegistration) InitializeConfig() any { return struct{}{} }
func (s closableCacheRegistration) Initialize(_ any, _ cache.CacheDeps) (cache.Cache, error) {
	return closableCache(s), nil
}

// The real caches live in internal/caches, which can't be imported here, so the registry tests
// build against a stub registered locally.
func init() {
	cache.RegisterCache(stubCacheRegistration{name: "stub-basic"})
}

func testDeps() cache.CacheDeps {
	return cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages}
}

func Test_CacheRegistry_SingleCacheGetsDefaultID(t *testing.T) {
	reg, err := ConstructCacheRegistry(context.Background(), map[string]interface{}{"name": "stub-basic"}, "", nil, testDeps())
	require.NoError(t, err)

	assert.Equal(t, "stub-basic", reg.DefaultID())
	assert.NotNil(t, reg.Default())

	_, ok := reg.Get("stub-basic")
	assert.True(t, ok)
}

func Test_CacheRegistry_ArrayDefaultsToFirstEntry(t *testing.T) {
	reg, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "first", "name": "stub-basic"},
		{"id": "second", "name": "stub-basic"},
	}, "", nil, testDeps())
	require.NoError(t, err)

	assert.Equal(t, "first", reg.DefaultID())
	assert.Equal(t, []string{"first", "second"}, reg.IDs())

	_, ok := reg.Get("second")
	assert.True(t, ok)
}

func Test_CacheRegistry_ExplicitDefault(t *testing.T) {
	reg, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "first", "name": "stub-basic"},
		{"id": "second", "name": "stub-basic"},
	}, "second", nil, testDeps())
	require.NoError(t, err)

	assert.Equal(t, "second", reg.DefaultID())
}

func Test_CacheRegistry_UnknownDefaultErrors(t *testing.T) {
	_, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "first", "name": "stub-basic"},
	}, "nope", nil, testDeps())

	require.ErrorContains(t, err, "nope")
}

func Test_CacheRegistry_MissingIDFallsBackToName(t *testing.T) {
	reg, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"name": "stub-basic"},
	}, "", nil, testDeps())
	require.NoError(t, err)

	_, ok := reg.Get("stub-basic")
	assert.True(t, ok)
	assert.Equal(t, "stub-basic", reg.DefaultID())
}

func Test_CacheRegistry_MissingIDAndNameErrors(t *testing.T) {
	_, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"maxsize": 10},
	}, "", nil, testDeps())

	require.ErrorContains(t, err, "cache[0].id")
}

func Test_CacheRegistry_DuplicateIDErrors(t *testing.T) {
	_, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "dupe", "name": "stub-basic"},
		{"id": "dupe", "name": "stub-basic"},
	}, "", nil, testDeps())

	require.ErrorContains(t, err, "dupe")
}

// A cache appearing once in the registry must be closed exactly once, since a double close on a
// pooled resource surfaces as errors on an unrelated shutdown path.
func Test_CacheRegistry_ClosesEachCacheOnce(t *testing.T) {
	closed := 0
	cache.RegisterCache(closableCacheRegistration{closed: &closed})

	reg, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{"id": "a", "name": "stub-closable"},
		{"id": "b", "name": "stub-closable"},
	}, "", nil, testDeps())
	require.NoError(t, err)

	require.NoError(t, reg.Close(context.Background()))
	assert.Equal(t, 2, closed)
}

func Test_CacheRegistry_NilIsSafe(t *testing.T) {
	var reg *CacheRegistry

	assert.Nil(t, reg.Default())
	assert.Empty(t, reg.DefaultID())
	assert.Empty(t, reg.IDs())
	require.NoError(t, reg.Close(context.Background()))

	_, ok := reg.Get("anything")
	assert.False(t, ok)
}

// A nil inner cache must not panic the walk looking for a TTL
func Test_CacheTTL_UnwrappedExpiringIsFound(t *testing.T) {
	ttl, ok := ExtractTTLFromCache(expiringCache{ttl: 90 * time.Second})

	assert.True(t, ok)
	assert.Equal(t, 90*time.Second, ttl)
}

func Test_CacheTTL_ZeroIsNotFound(t *testing.T) {
	ttl, ok := ExtractTTLFromCache(CacheWrapper{Name: "third-party", Cache: expiringCache{}})

	assert.False(t, ok)
	assert.Zero(t, ttl)
}

func Test_CacheTTL_ShortestNestedWins(t *testing.T) {
	tree := parentCache{children: []cache.Cache{
		expiringCache{ttl: time.Hour},
		CacheWrapper{Name: "third-party", Cache: expiringCache{ttl: time.Minute}},
		nil,
	}}

	ttl, ok := ExtractTTLFromCache(tree)

	assert.True(t, ok)
	assert.Equal(t, time.Minute, ttl)
}

func Test_CacheTTL_NoExpiringIsNotFound(t *testing.T) {
	ttl, ok := ExtractTTLFromCache(CacheWrapper{Name: "stub", Cache: stubCache{}})

	assert.False(t, ok)
	assert.Zero(t, ttl)
}

func Test_IsKeyedByIdentity_ThirdPartyAtAnyDepth(t *testing.T) {
	keyed := CacheWrapper{Name: "third-party", Cache: identityCache{keyed: true}}

	assert.True(t, IsKeyedByIdentity(keyed))
	assert.True(t, IsKeyedByIdentity(parentCache{children: []cache.Cache{stubCache{}, keyed}}))
	assert.False(t, IsKeyedByIdentity(parentCache{children: []cache.Cache{identityCache{keyed: false}}}))
	assert.False(t, IsKeyedByIdentity(nil))
}

func Test_IsNoop_SeesThroughDecoratorsOnly(t *testing.T) {
	noop := CacheWrapper{Name: "third-party", Cache: noopCache{}}

	assert.True(t, IsNoop(noop))
	assert.False(t, IsNoop(parentCache{children: []cache.Cache{noop}}))
	assert.False(t, IsNoop(stubCache{}))
	assert.False(t, IsNoop(nil))
}

func Test_ContainsCache_MatchesRegistrationName(t *testing.T) {
	tree := parentCache{children: []cache.Cache{CacheWrapper{Name: "wanted", Cache: stubCache{}}}}

	assert.True(t, ContainsCache(tree, "wanted"))
	assert.False(t, ContainsCache(tree, "other"))
}

func Test_ReflectedChildren_LegacyFieldsAreWalked(t *testing.T) {
	keyed := identityCache{keyed: true}

	assert.True(t, IsKeyedByIdentity(&legacySingle{Cache: keyed}))
	assert.True(t, IsKeyedByIdentity(legacyTiers{Tiers: []cache.Cache{stubCache{}, keyed}}))
	assert.False(t, IsKeyedByIdentity((*legacySingle)(nil)))
	assert.False(t, IsKeyedByIdentity(funcCache(nil)))
	assert.False(t, IsKeyedByIdentity(&legacySingle{}))
}

func Test_CacheRegistry_NestedThirdPartyParentIsReferenceable(t *testing.T) {
	cache.RegisterCache(parentCacheRegistration{})

	reg, err := ConstructCacheRegistry(context.Background(), []map[string]interface{}{
		{
			"id":   "outer",
			"name": "stub-parent",
			"children": []interface{}{
				map[string]interface{}{"id": "second", "name": "stub-basic"},
				map[string]interface{}{"name": "stub-basic"},
			},
		},
	}, "", nil, testDeps())
	require.NoError(t, err)

	for _, id := range []string{"outer", "second", "stub-basic"} {
		_, ok := reg.Get(id)
		assert.True(t, ok, "expected cache %v to be referenceable", id)
	}
}

type expiringCache struct {
	stubCache
	ttl time.Duration
}

func (c expiringCache) TTL() time.Duration { return c.ttl }

type identityCache struct {
	stubCache
	keyed bool
}

func (c identityCache) KeyedByIdentity() bool { return c.keyed }

type noopCache struct{ stubCache }

func (noopCache) IsNoop() bool { return true }

type parentCache struct {
	stubCache
	children []cache.Cache
}

func (c parentCache) Children() []cache.Cache { return c.children }

type parentCacheConfig struct {
	Children []map[string]interface{}
}

type parentCacheRegistration struct{}

func (parentCacheRegistration) Name() string          { return "stub-parent" }
func (parentCacheRegistration) InitializeConfig() any { return parentCacheConfig{} }
func (parentCacheRegistration) Initialize(cfgAny any, deps cache.CacheDeps) (cache.Cache, error) {
	cfg := cfgAny.(parentCacheConfig)
	built := make([]cache.Cache, 0, len(cfg.Children))

	// Built in reverse so registration can't rely on configuration order.
	for i := len(cfg.Children) - 1; i >= 0; i-- {
		child, err := ConstructCache(cfg.Children[i], deps)
		if err != nil {
			return nil, err
		}

		built = append(built, child)
	}

	return parentCache{children: built}, nil
}

type legacySingle struct {
	stubCache
	Cache cache.Cache
}

type legacyTiers struct {
	stubCache
	Tiers []cache.Cache
}

type funcCache func()

func (funcCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) { return nil, nil }
func (funcCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error   { return nil }
func (funcCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error)       { return false, nil }
