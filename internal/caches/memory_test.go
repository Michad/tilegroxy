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

package caches

import (
	"context"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/stretchr/testify/require"
)

type stubMemoryWrongType struct{}

func (stubMemoryWrongType) GetID() string { return "wrong" }
func (stubMemoryWrongType) Native() any   { return "not-an-otter-cache" }

type stubMemoryWrongTypeRegistration struct{}

func (stubMemoryWrongTypeRegistration) Name() string          { return "stub-memory-wrongtype" }
func (stubMemoryWrongTypeRegistration) InitializeConfig() any { return struct{ ID string }{} }
func (stubMemoryWrongTypeRegistration) Initialize(_ any, _ datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	return stubMemoryWrongType{}, nil
}

func init() {
	datastore.RegisterDatastoreWrapper(stubMemoryWrongTypeRegistration{})
}

func TestMemory(t *testing.T) {
	cfg := MemoryConfig{}

	r, err := MemoryRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	validateSaveAndLookup(t, r)
	validateRemove(t, r)
}

func TestTtl(t *testing.T) {
	cfg := MemoryConfig{TTL: 1}

	r, err := MemoryRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	tile := makeReq(53)
	img := makeImg(53)

	require.NoError(t, r.Save(context.Background(), tile, &img))

	validateLookup(t, r, tile, &img)
	time.Sleep(time.Duration(2) * time.Second)
	validateNoLookup(t, r, tile)
}

// We intentionally don't test the maxsize property as the otter library doesn't offer guarantees on how capacity settings are honored.  See https://github.com/maypok86/otter/issues/88 for more details

func memoryDatastoreRegistry(t *testing.T, id string) *datastore.DatastoreRegistry {
	t.Helper()

	reg, err := datastore.ConstructDatastoreRegistry([]map[string]interface{}{{"name": "memory", "id": id}}, nil, config.ErrorMessages{})
	require.NoError(t, err)

	return reg
}

// The point of the memory datastore: two separately defined memory caches, as happens when a multi
// cache is duplicated to wrap one copy in tenant, hold the same tiles instead of silently diverging.
func TestMemoryDatastoreIsSharedBetweenCaches(t *testing.T) {
	deps := cache.CacheDeps{ErrorMessages: config.ErrorMessages{}, Datastores: memoryDatastoreRegistry(t, "shared")}

	first, err := MemoryRegistration{}.Initialize(MemoryConfig{Datastore: "shared"}, deps)
	require.NoError(t, err)

	second, err := MemoryRegistration{}.Initialize(MemoryConfig{Datastore: "shared"}, deps)
	require.NoError(t, err)

	tile := makeReq(61)
	img := makeImg(61)

	require.NoError(t, first.Save(context.Background(), tile, &img))
	validateLookup(t, second, tile, &img)

	removed, err := second.Remove(context.Background(), tile)
	require.NoError(t, err)
	require.True(t, removed)

	validateNoLookup(t, first, tile)
}

// Without a datastore each cache keeps its own store, so one cache's tiles stay invisible to another.
func TestMemoryWithoutDatastoreIsPrivate(t *testing.T) {
	deps := cache.CacheDeps{ErrorMessages: config.ErrorMessages{}}

	first, err := MemoryRegistration{}.Initialize(MemoryConfig{}, deps)
	require.NoError(t, err)

	second, err := MemoryRegistration{}.Initialize(MemoryConfig{}, deps)
	require.NoError(t, err)

	tile := makeReq(62)
	img := makeImg(62)

	require.NoError(t, first.Save(context.Background(), tile, &img))
	validateNoLookup(t, second, tile)
}

func TestMemoryDatastoreNotFound(t *testing.T) {
	deps := cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages, Datastores: memoryDatastoreRegistry(t, "other")}

	_, err := MemoryRegistration{}.Initialize(MemoryConfig{Datastore: "missing"}, deps)
	require.ErrorContains(t, err, "missing")
}

func TestMemoryDatastoreWrongType(t *testing.T) {
	reg, err := datastore.ConstructDatastoreRegistry([]map[string]interface{}{{"name": "stub-memory-wrongtype", "id": "wrong"}}, nil, config.ErrorMessages{})
	require.NoError(t, err)

	deps := cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages, Datastores: reg}

	_, err = MemoryRegistration{}.Initialize(MemoryConfig{Datastore: "wrong"}, deps)
	require.ErrorContains(t, err, "wrong")
}

func TestMemoryDatastoreRejectsSizingParams(t *testing.T) {
	deps := cache.CacheDeps{ErrorMessages: config.DefaultConfig().Error.Messages, Datastores: memoryDatastoreRegistry(t, "shared")}

	_, err := MemoryRegistration{}.Initialize(MemoryConfig{Datastore: "shared", TTL: 5}, deps)
	require.ErrorContains(t, err, "cache.memory.datastore")

	_, err = MemoryRegistration{}.Initialize(MemoryConfig{Datastore: "shared", MaxSize: 20}, deps)
	require.ErrorContains(t, err, "cache.memory.datastore")
}

// A private store owns its otter goroutines; a shared one belongs to the datastore registry and
// must survive the cache closing.
func TestMemoryCloseOnlyClosesPrivateStore(t *testing.T) {
	deps := cache.CacheDeps{ErrorMessages: config.ErrorMessages{}, Datastores: memoryDatastoreRegistry(t, "shared")}

	shared, err := MemoryRegistration{}.Initialize(MemoryConfig{Datastore: "shared"}, deps)
	require.NoError(t, err)

	require.NoError(t, lifecycle.CloseIfCloser(context.Background(), shared))

	tile := makeReq(63)
	img := makeImg(63)
	require.NoError(t, shared.Save(context.Background(), tile, &img))
	validateLookup(t, shared, tile, &img)

	private, err := MemoryRegistration{}.Initialize(MemoryConfig{}, deps)
	require.NoError(t, err)
	require.NoError(t, lifecycle.CloseIfCloser(context.Background(), private))
}
