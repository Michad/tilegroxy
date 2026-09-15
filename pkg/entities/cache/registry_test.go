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
	reg, err := ConstructCacheRegistry(map[string]interface{}{"name": "stub-basic"}, "", nil, testDeps())
	require.NoError(t, err)

	assert.Equal(t, config.DefaultCacheID, reg.DefaultID())
	assert.NotNil(t, reg.Default())

	_, ok := reg.Get(config.DefaultCacheID)
	assert.True(t, ok)
}

func Test_CacheRegistry_ArrayDefaultsToFirstEntry(t *testing.T) {
	reg, err := ConstructCacheRegistry([]map[string]interface{}{
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
	reg, err := ConstructCacheRegistry([]map[string]interface{}{
		{"id": "first", "name": "stub-basic"},
		{"id": "second", "name": "stub-basic"},
	}, "second", nil, testDeps())
	require.NoError(t, err)

	assert.Equal(t, "second", reg.DefaultID())
}

func Test_CacheRegistry_UnknownDefaultErrors(t *testing.T) {
	_, err := ConstructCacheRegistry([]map[string]interface{}{
		{"id": "first", "name": "stub-basic"},
	}, "nope", nil, testDeps())

	require.ErrorContains(t, err, "nope")
}

func Test_CacheRegistry_MissingIDFallsBackToName(t *testing.T) {
	reg, err := ConstructCacheRegistry([]map[string]interface{}{
		{"name": "stub-basic"},
	}, "", nil, testDeps())
	require.NoError(t, err)

	_, ok := reg.Get("stub-basic")
	assert.True(t, ok)
	assert.Equal(t, "stub-basic", reg.DefaultID())
}

func Test_CacheRegistry_MissingIDAndNameErrors(t *testing.T) {
	_, err := ConstructCacheRegistry([]map[string]interface{}{
		{"maxsize": 10},
	}, "", nil, testDeps())

	require.ErrorContains(t, err, "cache[0].id")
}

func Test_CacheRegistry_DuplicateIDErrors(t *testing.T) {
	_, err := ConstructCacheRegistry([]map[string]interface{}{
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

	reg, err := ConstructCacheRegistry([]map[string]interface{}{
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

func Test_NewSingleCacheRegistry(t *testing.T) {
	reg := NewSingleCacheRegistry(stubCache{})

	assert.Equal(t, config.DefaultCacheID, reg.DefaultID())
	assert.NotNil(t, reg.Default())
}
