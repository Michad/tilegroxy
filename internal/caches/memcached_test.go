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

//go:build !unit

package caches

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	// Lets vscode test runs pick up testcontainer settings from a .env in the repo root
	if env, err := os.ReadFile("../../.env"); err == nil {
		envs := strings.Split(string(env), "\n")
		for _, e := range envs {
			if es := strings.Split(e, "="); len(es) == 2 {
				fmt.Printf("Loading env...")
				os.Setenv(es[0], es[1])
			}
		}
	}
}

func setupMemcachedContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
	t.Log("setup container")

	req := testcontainers.ContainerRequest{
		Image:        "memcached:latest",
		ExposedPorts: []string{"11211/tcp"},
		WaitingFor:   wait.ForExposedPort(),
	}
	memcachedC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	return memcachedC, func(t *testing.T) {
		t.Log("teardown container")

		err := memcachedC.Terminate(ctx)
		require.NoError(t, err)
	}
}

func TestMemcachedWithContainerHostAndPort(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	require.NoError(t, err)

	cfg := MemcachedConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	validateSaveAndLookup(t, r)
	validateRemove(t, r)
}

func TestMemcachedWithContainerSingleServersArr(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	require.NoError(t, err)

	cfg := MemcachedConfig{
		Servers: []HostAndPort{extractHostAndPort(t, endpoint)},
	}

	r, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	validateSaveAndLookup(t, r)
}

func TestMemcachedWithContainerDiffPrefix(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	require.NoError(t, err)

	cfg := MemcachedConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "first_",
	}

	r, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	config2 := MemcachedConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "second_",
	}

	r2, err := MemcachedRegistration{}.Initialize(config2, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	validateSaveAndLookup(t, r)
	validateSaveAndLookup(t, r2)
}

// Reporting a miss as an error logs a warning on every miss and buries real failures
func TestMemcachedWithContainerMissIsNotAnError(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	require.NoError(t, err)

	cfg := MemcachedConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	validateNoLookup(t, r, makeReq(1))
}

func TestMemcachedWithContainerUsingDatastore(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	require.NoError(t, err)

	hostAndPort := extractHostAndPort(t, endpoint)

	dsCfg := []map[string]interface{}{
		{
			"name": "memcached",
			"id":   "mymemcache",
			"host": hostAndPort.Host,
			"port": hostAndPort.Port,
		},
	}

	reg, err := datastores.ConstructDatastoreRegistry(context.Background(), dsCfg, nil, config.ErrorMessages{})
	require.NoError(t, err)
	defer func() { require.NoError(t, reg.Close(ctx)) }()

	cfg := MemcachedConfig{Datastore: "mymemcache"}

	r, err := MemcachedRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}, Datastores: reg})
	require.NoError(t, err)

	validateSaveAndLookup(t, r)

	// Closing the cache must not close the shared datastore connection under other consumers
	require.NoError(t, r.(interface {
		Close(ctx context.Context) error
	}).Close(ctx))
	validateSaveAndLookup(t, r)
}

// "memcache" stays an alias for backwards compatibility, for both the cache and its datastore
func TestMemcachedWithContainerLegacyMemcacheNameAlias(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	require.NoError(t, err)

	hostAndPort := extractHostAndPort(t, endpoint)

	dsCfg := []map[string]interface{}{
		{
			"name": "memcache",
			"id":   "mymemcache",
			"host": hostAndPort.Host,
			"port": hostAndPort.Port,
		},
	}

	reg, err := datastores.ConstructDatastoreRegistry(context.Background(), dsCfg, nil, config.ErrorMessages{})
	require.NoError(t, err)
	defer func() { require.NoError(t, reg.Close(ctx)) }()

	c, err := ConstructCache(map[string]interface{}{"name": "memcache", "datastore": "mymemcache"}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}, Datastores: reg})
	require.NoError(t, err)

	validateSaveAndLookup(t, c)
}
