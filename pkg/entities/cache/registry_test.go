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

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
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
func (s closableCacheRegistration) Initialize(_ any, _ CacheDeps) (Cache, error) {
	return closableCache(s), nil
}

// The real caches live in internal/caches, which can't be imported here, so the registry tests
// build against a stub registered locally.
func init() {
	RegisterCache(stubCacheRegistration{name: "stub-basic"})
}

func testDeps() CacheDeps {
	return CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages}
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
	RegisterCache(closableCacheRegistration{closed: &closed})

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
func Test_CacheTTL_NilCacheIsNotFound(t *testing.T) {
	ttl, ok := ExtractTTLFromCache(CacheWrapper{Name: "ttl", Cache: (*nilInnerCache)(nil)})

	assert.False(t, ok)
	assert.Zero(t, ttl)
}

// The ttl cache is held by pointer, so the walk has to dereference to reach its field
func Test_CacheTTL_ReadsThroughPointer(t *testing.T) {
	ttl, ok := ExtractTTLFromCache(CacheWrapper{Name: "ttl", Cache: &nilInnerCache{TTL: 90 * time.Second}})

	assert.True(t, ok)
	assert.Equal(t, 90*time.Second, ttl)
}

// A ttl cache with no TTL field at all, such as one that isn't a struct
func Test_CacheTTL_NonStructIsNotFound(t *testing.T) {
	ttl, ok := ExtractTTLFromCache(CacheWrapper{Name: "ttl", Cache: funcCache(nil)})

	assert.False(t, ok)
	assert.Zero(t, ttl)
}

// A ttl cache whose TTL field isn't a duration says nothing about freshness
func Test_CacheTTL_NonDurationFieldIsNotFound(t *testing.T) {
	ttl, ok := ExtractTTLFromCache(CacheWrapper{Name: "ttl", Cache: oddTTLCache{}})

	assert.False(t, ok)
	assert.Zero(t, ttl)
}

type nilInnerCache struct{ TTL time.Duration }

func (*nilInnerCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
func (*nilInnerCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error { return nil }
func (*nilInnerCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error)     { return false, nil }

type oddTTLCache struct{ TTL string }

func (oddTTLCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) { return nil, nil }
func (oddTTLCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error   { return nil }
func (oddTTLCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error)       { return false, nil }

type funcCache func()

func (funcCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) { return nil, nil }
func (funcCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error   { return nil }
func (funcCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error)       { return false, nil }
