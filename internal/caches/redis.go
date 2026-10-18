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
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg"

	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	rediscache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

// Inline connection info is intentionally undocumented, kept functional for compatibility
type RedisConfig struct {
	HostAndPort `mapstructure:",squash"` // Host and Port for a single server. A convenience equivalent to supplying Servers with a single entry
	DB          int                      // Database number, defaults to 0
	KeyPrefix   string                   // Prefix to keynames stored in cache
	Username    string                   // Username to use to authenticate
	Password    string                   // Password to use to authenticate
	Mode        string                   // Controls operating mode. One of datastores.AllRedisModes. Defaults to standalone
	TTL         uint32                   // Cache expiration in seconds. Max of 1 year. Default to 1 day
	Servers     []HostAndPort            // The list of servers to use.
	Datastore   string                   // ID of a redis datastore to reuse instead of connecting directly. Mutually exclusive with the connection parameters above
}

const (
	redisDefaultTTL = 60 * 60 * 24
	redisMaxTTL     = 60 * 60 * 24 * 365
)

type Redis struct {
	RedisConfig
	cache *rediscache.Cache
	// rediscache.Cache doesn't expose its client, so it's retained for shutdown. Nil for a shared datastore, which the registry closes
	client io.Closer
	// Remove needs DEL's reply count, which rediscache.Cache discards. Always set, unlike client
	redis redis.UniversalClient
}

func init() {
	cache.RegisterCache(RedisRegistration{})
}

type RedisRegistration struct {
}

func (s RedisRegistration) InitializeConfig() any {
	return RedisConfig{}
}

func (s RedisRegistration) Name() string {
	return "redis"
}

func (s RedisRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(RedisConfig)

	config.TTL = clampRedisTTL(config.TTL)

	if config.Datastore != "" {
		return initializeRedisFromDatastore(config, deps)
	}

	return initializeRedisDirect(config, deps)
}

func clampRedisTTL(ttl uint32) uint32 {
	if ttl == 0 {
		return redisDefaultTTL
	}
	if ttl > redisMaxTTL {
		return redisMaxTTL
	}

	return ttl
}

func initializeRedisFromDatastore(config RedisConfig, deps cache.CacheDeps) (cache.Cache, error) {
	if config.Host != "" || len(config.Servers) > 0 || config.Username != "" || config.Password != "" || config.Mode != "" || config.DB != 0 {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "cache.redis.datastore", "cache.redis connection parameters")
	}

	ds, ok := deps.Datastores.Get(config.Datastore)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.redis.datastore", config.Datastore)
	}

	client, ok := ds.Native().(redis.UniversalClient)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.redis.datastore", config.Datastore)
	}

	return &Redis{RedisConfig: config, cache: newRedisTileCache(client), client: nil, redis: client}, nil
}

// Builds a private, unregistered redis datastore from the inline fields to reuse its connection logic
func initializeRedisDirect(config RedisConfig, deps cache.CacheDeps) (cache.Cache, error) {
	servers := make([]datastores.RedisHostAndPort, len(config.Servers))
	for i, s := range config.Servers {
		servers[i] = datastores.RedisHostAndPort{Host: s.Host, Port: s.Port}
	}

	dsConfig := datastores.RedisWrapperConfig{
		Host:     config.Host,
		Port:     config.Port,
		DB:       config.DB,
		Username: config.Username,
		Password: config.Password,
		Mode:     config.Mode,
		Servers:  servers,
	}

	ds, err := datastores.RedisWrapperRegistration{}.Initialize(dsConfig, datastore.DatastoreDeps{ErrorMessages: deps.ErrorMessages})
	if err != nil {
		return nil, err
	}

	client := ds.Native().(redis.UniversalClient)

	return &Redis{RedisConfig: config, cache: newRedisTileCache(client), client: client, redis: client}, nil
}

func newRedisTileCache(client redis.UniversalClient) *rediscache.Cache {
	return rediscache.New(&rediscache.Options{
		Redis: client,
	})
}

func (c Redis) Close(_ context.Context) error {
	if c.client == nil {
		return nil
	}

	return c.client.Close()
}

func (c Redis) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	key := c.KeyPrefix + t.String()
	var b []byte

	err := c.cache.Get(ctx, key, &b)

	if errors.Is(err, rediscache.ErrCacheMiss) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return pkg.DecodeImage(b)
}

func (c Redis) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	key := c.KeyPrefix + t.String()
	val, err := img.Encode()

	if err != nil {
		return err
	}

	err = c.cache.Set(&rediscache.Item{
		Ctx:   ctx,
		Key:   key,
		Value: val,
		TTL:   time.Duration(c.TTL) * time.Second,
	})

	return err
}

// Goes to the client directly since rediscache.Cache discards DEL's removed count
func (c Redis) Remove(ctx context.Context, t pkg.TileRequest) (bool, error) {
	key := c.KeyPrefix + t.String()

	deleted, err := c.redis.Del(ctx, key).Result()

	if errors.Is(err, rediscache.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return deleted > 0, nil
}
