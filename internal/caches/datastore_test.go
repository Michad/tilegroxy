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

	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubDatastoreWrapper struct {
	id     string
	native any
}

func (s stubDatastoreWrapper) GetID() string { return s.id }
func (s stubDatastoreWrapper) Native() any   { return s.native }

type stubDatastoreRegistration struct {
	name   string
	native any
}

func (s stubDatastoreRegistration) Name() string          { return s.name }
func (s stubDatastoreRegistration) InitializeConfig() any { return struct{ ID string }{} }
func (s stubDatastoreRegistration) Initialize(cfgAny any, _ datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(struct{ ID string })
	return stubDatastoreWrapper{id: cfg.ID, native: s.native}, nil
}

func buildDatastoreRegistry(t *testing.T, id string, regName string, native any) *datastore.DatastoreRegistry {
	t.Helper()

	datastore.RegisterDatastoreWrapper(stubDatastoreRegistration{name: regName, native: native})

	reg, err := datastore.ConstructDatastoreRegistry([]map[string]interface{}{{"name": regName, "id": id}}, nil, config.ErrorMessages{})
	require.NoError(t, err)

	return reg
}

var errorMessages = config.ErrorMessages{
	InvalidParam:            "Invalid value supplied for parameter %v: %v",
	ParamsMutuallyExclusive: "Parameters %v and %v cannot both be set",
}

func TestRedisDatastoreAndHostMutuallyExclusive(t *testing.T) {
	cfg := RedisConfig{
		Datastore:   "myredis",
		HostAndPort: HostAndPort{Host: "127.0.0.1"},
	}

	_, err := RedisRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: errorMessages})
	require.Error(t, err)
}

func TestRedisDatastoreNotFound(t *testing.T) {
	reg := buildDatastoreRegistry(t, "other", "stub-redis-notfound", nil)

	cfg := RedisConfig{Datastore: "myredis"}

	_, err := RedisRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: errorMessages, Datastores: reg})
	require.Error(t, err)
}

func TestRedisDatastoreWrongType(t *testing.T) {
	reg := buildDatastoreRegistry(t, "myredis", "stub-redis-wrongtype", "not-a-redis-client")

	cfg := RedisConfig{Datastore: "myredis"}

	_, err := RedisRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: errorMessages, Datastores: reg})
	require.Error(t, err)
}

func TestMemcachedDatastoreAndHostMutuallyExclusive(t *testing.T) {
	cfg := MemcachedConfig{
		Datastore:   "mymemcache",
		HostAndPort: HostAndPort{Host: "127.0.0.1"},
	}

	_, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: errorMessages})
	require.Error(t, err)
}

func TestMemcachedDatastoreNotFound(t *testing.T) {
	reg := buildDatastoreRegistry(t, "other", "stub-memcache-notfound", nil)

	cfg := MemcachedConfig{Datastore: "mymemcache"}

	_, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: errorMessages, Datastores: reg})
	require.Error(t, err)
}

func TestMemcachedDatastoreWrongType(t *testing.T) {
	reg := buildDatastoreRegistry(t, "mymemcache", "stub-memcache-wrongtype", "not-a-memcache-client")

	cfg := MemcachedConfig{Datastore: "mymemcache"}

	_, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: errorMessages, Datastores: reg})
	require.Error(t, err)
}

// Close must not close the datastore's shared client, since the registry owns its lifecycle.
func TestMemcachedDatastoreCloseDoesNotCloseSharedClient(t *testing.T) {
	client := memcache.New("127.0.0.1:11211")
	reg := buildDatastoreRegistry(t, "mymemcache", "stub-memcache-close", client)

	cfg := MemcachedConfig{Datastore: "mymemcache"}

	c, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: errorMessages, Datastores: reg})
	require.NoError(t, err)

	require.False(t, c.(*Memcached).ownsClient)
	require.NoError(t, c.(*Memcached).Close(context.Background()))
}

func TestMemcachedNames(t *testing.T) {
	assert.Equal(t, "memcached", MemcachedRegistration{}.Name())
	assert.Equal(t, "memcache", MemcachedLegacyRegistration{}.Name())

	for _, name := range []string{"memcached", "memcache"} {
		reg, ok := cache.RegisteredCache(name)
		require.True(t, ok, name)
		assert.IsType(t, MemcachedConfig{}, reg.InitializeConfig())
	}
}

func TestMemcachedWrapperNames(t *testing.T) {
	assert.Equal(t, "memcached", datastores.MemcachedWrapperRegistration{}.Name())
	assert.Equal(t, "memcache", datastores.MemcachedWrapperLegacyRegistration{}.Name())

	for _, name := range []string{"memcached", "memcache"} {
		reg, ok := datastore.RegisteredDatastoreWrapper(name)
		require.True(t, ok, name)
		assert.IsType(t, datastores.MemcachedWrapperConfig{}, reg.InitializeConfig())
	}
}
