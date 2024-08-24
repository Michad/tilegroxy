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
	"slices"
	"strconv"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"

	"github.com/Michad/tilegroxy/pkg/entities/cache"
	rediscache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

const (
	ModeStandalone = "standalone"
	ModeCluster    = "cluster"
	ModeRing       = "ring"
)

var AllModes = []string{ModeStandalone, ModeCluster, ModeRing}

type RedisConfig struct {
	HostAndPort `mapstructure:",squash"` // Host and Port for a single server. A convenience equivalent to supplying Servers with a single entry
	DB          int                      // Database number, defaults to 0
	KeyPrefix   string                   // Prefix to keynames stored in cache
	Username    string                   // Username to use to authenticate
	Password    string                   // Password to use to authenticate
	Mode        string                   // Controls operating mode. One of AllModes. Defaults to standalone
	TTL         uint32                   // Cache expiration in seconds. Max of 1 year. Default to 1 day
	Servers     []HostAndPort            // The list of servers to use.
}

const (
	redisDefaultHost = "127.0.0.1"
	redisDefaultPort = 6379
	redisDefaultTTL  = 60 * 60 * 24
	redisMaxTTL      = 60 * 60 * 24 * 365
)

type Redis struct {
	RedisConfig
	cache *rediscache.Cache
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

func (s RedisRegistration) Initialize(configAny any, errorMessages config.ErrorMessages) (cache.Cache, error) {
	config := configAny.(RedisConfig)

	var tileCache *rediscache.Cache

	if config.Mode == "" {
		config.Mode = ModeStandalone
	}

	if !slices.Contains(AllModes, config.Mode) {
		return nil, fmt.Errorf(errorMessages.EnumError, "cache.redis.mode", config.Mode, AllModes)
	}

	if config.Servers == nil || len(config.Servers) == 0 {
		if config.Host == "" {
			config.Host = redisDefaultHost
		}
		if config.Port == 0 {
			config.Port = redisDefaultPort
		}

		config.Servers = []HostAndPort{{config.Host, config.Port}}
	} else if config.Host != "" {
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "config.redis.host", "config.redis.servers")
	}

	if config.TTL == 0 {
		config.TTL = redisDefaultTTL
	}
	if config.TTL > redisMaxTTL {
		config.TTL = redisMaxTTL
	}

	switch config.Mode {
	case ModeCluster:
		if config.DB != 0 {
			return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "cache.redis.db", "cache.redis.cluster")
		}

		addrs := HostAndPortArrayToStringArray(config.Servers)

		client := redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    addrs,
			Username: config.Username,
			Password: config.Password,
		})

		//TODO: Open bug with go-redis about `rediser` type being private so the below isn't needlessly repeated
		tileCache = rediscache.New(&rediscache.Options{
			Redis: client,
		})
	case ModeRing:
		if len(config.Servers) < 2 {
			// Not the best error message but the typical user of this should be able to figure it out
			return nil, fmt.Errorf(errorMessages.InvalidParam, "length(cache.redis.servers)", len(config.Servers))
		}

		addrMap := make(map[string]string)
		for _, addr := range config.Servers {
			addrMap[addr.Host] = ":" + strconv.Itoa(int(addr.Port))
		}

		client := redis.NewRing(&redis.RingOptions{
			Addrs:    addrMap,
			Username: config.Username,
			Password: config.Password,
			DB:       config.DB,
		})

		//TODO: Open bug with go-redis about `rediser` type being private so the below isn't needlessly repeated
		tileCache = rediscache.New(&rediscache.Options{
			Redis: client,
		})
	default:
		client := redis.NewClient(&redis.Options{
			Addr:     config.Servers[0].Host + ":" + strconv.Itoa(int(config.Servers[0].Port)),
			Username: config.Username,
			Password: config.Password,
			DB:       config.DB,
		})

		//TODO: Open bug with go-redis about `rediser` type being private so the below isn't needlessly repeated
		tileCache = rediscache.New(&rediscache.Options{
			Redis: client,
		})
	}

	r := Redis{RedisConfig: config, cache: tileCache}

	return &r, nil
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
