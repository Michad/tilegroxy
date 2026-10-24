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

//go:build !unit

package ratelimit

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Must stay in sync with ratelimit/postgresql.adoc
const postgresDDL = `CREATE TABLE tilegroxy_ratelimit (
    key           TEXT        NOT NULL,
    window_start  TIMESTAMPTZ NOT NULL,
    count         BIGINT      NOT NULL,
    expires       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (key, window_start)
);

CREATE INDEX ON tilegroxy_ratelimit (expires);`

func startContainer(t *testing.T, req testcontainers.ContainerRequest) (string, string) {
	t.Helper()

	ctx := context.Background()

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, c.Terminate(context.Background())) })

	endpoint, err := c.Endpoint(ctx, "")
	require.NoError(t, err)

	host, port, _ := strings.Cut(endpoint, ":")

	return host, port
}

func registryFor(t *testing.T, ds map[string]interface{}) *datastores.Registry {
	t.Helper()

	reg, err := datastores.ConstructDatastoreRegistry(context.Background(), []map[string]interface{}{ds}, nil, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	t.Cleanup(func() { _ = reg.Close(context.Background()) })

	return reg
}

func setupRedis(t *testing.T) *datastores.Registry {
	t.Helper()

	host, port := startContainer(t, testcontainers.ContainerRequest{
		Image:        "redis:latest",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	})

	portNum, err := strconv.Atoi(port)
	require.NoError(t, err)

	return registryFor(t, map[string]interface{}{"name": "redis", "id": "redis-0", "host": host, "port": portNum})
}

func setupPostgres(t *testing.T) *datastores.Registry {
	t.Helper()

	host, port := startContainer(t, testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_PASSWORD": "hunter2", "POSTGRES_USER": "postgres", "POSTGRES_DB": "postgres"},
		WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2 * time.Minute),
	})

	portNum, err := strconv.Atoi(port)
	require.NoError(t, err)

	reg := registryFor(t, map[string]interface{}{"name": "postgresql", "id": "pg", "database": "postgres", "host": host, "port": portNum, "user": "postgres", "password": "hunter2"})

	ds, ok := reg.Get("pg")
	require.True(t, ok)

	pool := ds.Native().(*pgxpool.Pool)

	for _, wait := range []int{0, 1, 2, 4, 8} {
		time.Sleep(time.Duration(wait) * time.Second)

		if _, err = pool.Exec(context.Background(), postgresDDL); err == nil {
			break
		}
	}

	require.NoError(t, err)

	return reg
}

func validateStore(t *testing.T, store ratelimit.RateLimiter) {
	t.Helper()

	ctx := context.Background()
	window := time.Now().Truncate(time.Second)

	a := ratelimit.RateLimitCounter{Key: "standard.user.1s:abc", Window: window, TTL: time.Minute}
	prev := ratelimit.RateLimitCounter{Key: a.Key, Window: window.Add(-time.Second), TTL: time.Minute}
	b := ratelimit.RateLimitCounter{Key: "premium.tenant.month:def", Window: window, TTL: time.Minute}
	short := ratelimit.RateLimitCounter{Key: "short", Window: window, TTL: 50 * time.Millisecond}

	counts, err := store.Get(ctx, []ratelimit.RateLimitCounter{a, prev, b})
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 0, 0}, counts)

	require.NoError(t, store.Increment(ctx, []ratelimit.RateLimitCounter{a, b}))
	require.NoError(t, store.Increment(ctx, []ratelimit.RateLimitCounter{b, a}))
	require.NoError(t, store.Increment(ctx, []ratelimit.RateLimitCounter{prev}))
	require.NoError(t, store.Increment(ctx, []ratelimit.RateLimitCounter{short}))

	counts, err = store.Get(ctx, []ratelimit.RateLimitCounter{a, prev, b, short})
	require.NoError(t, err)
	assert.Equal(t, []int64{2, 1, 2, 1}, counts)

	time.Sleep(time.Second)

	counts, err = store.Get(ctx, []ratelimit.RateLimitCounter{short})
	require.NoError(t, err)
	assert.Equal(t, []int64{0}, counts, "expired")

	require.NoError(t, store.Increment(ctx, []ratelimit.RateLimitCounter{{Key: short.Key, Window: short.Window, TTL: time.Minute}}))
	counts, err = store.Get(ctx, []ratelimit.RateLimitCounter{short})
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, counts, "an expired count starts over")

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, store.Increment(ctx, []ratelimit.RateLimitCounter{a, b}))
		}()
	}
	wg.Wait()

	counts, err = store.Get(ctx, []ratelimit.RateLimitCounter{a, b})
	require.NoError(t, err)
	assert.Equal(t, []int64{22, 22}, counts, "concurrent increments are never lost")

	clock, ok := store.(ratelimit.Clock)
	require.True(t, ok)

	storeNow, err := clock.Now(ctx)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), storeNow, 5*time.Second)

	resetter, ok := store.(ratelimit.RateLimitResetter)
	require.True(t, ok)
	require.NoError(t, resetter.Reset(ctx, []ratelimit.RateLimitCounter{a, prev}))

	counts, err = store.Get(ctx, []ratelimit.RateLimitCounter{a, prev, b})
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 0, 22}, counts)
}

func Test_Redis_Store(t *testing.T) {
	reg := setupRedis(t)

	store, err := RedisRegistration{}.Initialize(RedisConfig{Datastore: "redis-0", KeyPrefix: redisDefaultKeyPrefix}, ratelimit.RateLimiterDeps{Datastores: reg, ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	validateStore(t, store)

	ds, _ := reg.Get("redis-0")
	keys, err := ds.Native().(redis.UniversalClient).Keys(context.Background(), "*").Result()
	require.NoError(t, err)

	for _, k := range keys {
		assert.True(t, strings.HasPrefix(k, redisDefaultKeyPrefix), k)

		ttl, err := ds.Native().(redis.UniversalClient).PTTL(context.Background(), k).Result()
		require.NoError(t, err)
		assert.Positive(t, ttl, "every key expires on its own")
	}
}

func Test_Redis_StoreFailure(t *testing.T) {
	reg := registryFor(t, map[string]interface{}{"name": "redis", "id": "redis-0", "host": "127.0.0.1", "port": 1})

	store, err := RedisRegistration{}.Initialize(RedisConfig{Datastore: "redis-0"}, ratelimit.RateLimiterDeps{Datastores: reg})
	require.NoError(t, err)

	_, err = store.Get(context.Background(), []ratelimit.RateLimitCounter{{Key: "a", Window: time.Now(), TTL: time.Second}})
	require.Error(t, err)
}

func Test_Redis_WrongDatastoreType(t *testing.T) {
	reg := registryFor(t, map[string]interface{}{"name": "memory", "id": "mem"})

	_, err := RedisRegistration{}.Initialize(RedisConfig{Datastore: "mem"}, ratelimit.RateLimiterDeps{Datastores: reg})
	require.Error(t, err)

	_, err = PostgresqlRegistration{}.Initialize(PostgresqlConfig{Datastore: "mem", Table: "t"}, ratelimit.RateLimiterDeps{Datastores: reg})
	require.Error(t, err)
}

func Test_Postgresql_Store(t *testing.T) {
	reg := setupPostgres(t)

	store, err := PostgresqlRegistration{}.Initialize(PostgresqlConfig{Datastore: "pg", Table: "public.tilegroxy_ratelimit"}, ratelimit.RateLimiterDeps{Datastores: reg, ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	validateStore(t, store)

	// A request reaching one counter twice still counts it once per call
	c := ratelimit.RateLimitCounter{Key: "dup", Window: time.Now().Truncate(time.Second), TTL: time.Minute}
	require.NoError(t, store.Increment(context.Background(), []ratelimit.RateLimitCounter{c, c}))

	counts, err := store.Get(context.Background(), []ratelimit.RateLimitCounter{c, c})
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 1}, counts)
}

func Test_Postgresql_MissingTable(t *testing.T) {
	reg := setupPostgres(t)

	store, err := PostgresqlRegistration{}.Initialize(PostgresqlConfig{Datastore: "pg", Table: "missing"}, ratelimit.RateLimiterDeps{Datastores: reg})
	require.NoError(t, err)

	c := []ratelimit.RateLimitCounter{{Key: "a", Window: time.Now(), TTL: time.Second}}

	_, err = store.Get(context.Background(), c)
	require.Error(t, err)
	require.Error(t, store.Increment(context.Background(), c))
	require.Error(t, store.(ratelimit.RateLimitResetter).Reset(context.Background(), c))
}

func Test_Redis_LimiterEndToEnd(t *testing.T) {
	reg := setupRedis(t)

	cfg := loadConfig(t, `
ratelimit:
  name: redis
  datastore: redis-0
  keyprefix: "e2e:"
  policies:
    - id: a
      rules:
        - {segment: [ip], limit: 3, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)

	l, err := ConstructLimiter(context.Background(), cfg, nil, ratelimit.RateLimiterDeps{Datastores: reg, ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	ctx := requestContext("", "", "10.0.0.1:1")

	for range 3 {
		require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
	}

	assert.Equal(t, Rejected, l.Check(ctx, "l").Outcome)

	// A second instance shares the counts
	other, err := ConstructLimiter(context.Background(), cfg, nil, ratelimit.RateLimiterDeps{Datastores: reg, ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)
	assert.Equal(t, Rejected, other.Check(ctx, "l").Outcome)

	usages, _, err := other.Inspect(context.Background(), InspectOptions{IP: "10.0.0.1"})
	require.NoError(t, err)
	require.Len(t, usages, 1)
	assert.Equal(t, int64(3), usages[0].Used)

	_, _, err = other.Reset(context.Background(), InspectOptions{IP: "10.0.0.1"}, false)
	require.NoError(t, err)
	assert.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
}
