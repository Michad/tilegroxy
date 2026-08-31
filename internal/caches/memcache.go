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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	memcacheDefaultHost = "127.0.0.1"
	memcacheDefaultPort = 11211
	memcacheDefaultTTL  = 60 * 60 * 24
	memcacheMaxTTL      = 30 * 60 * 60 * 24
)

// Note: inline connection info is intentionally undocumented, just left functional for compatibility
type MemcacheConfig struct {
	HostAndPort `mapstructure:",squash"`
	Servers     []HostAndPort // The list of servers to use.
	KeyPrefix   string        // Prefix to keynames stored in cache
	TTL         uint          // Cache expiration in seconds. Max of 30 days. Default to 1 day
	Datastore   string        // ID of a memcache datastore to reuse instead of connecting directly. Mutually exclusive with the connection parameters above
}

type Memcache struct {
	MemcacheConfig
	client *memcache.Client
	// Whether client was created for this cache alone, and therefore should be closed with it.
	// False when client came from a shared datastore, since the datastore registry owns closing it.
	ownsClient bool
}

func init() {
	cache.RegisterCache(MemcacheRegistration{})
}

type MemcacheRegistration struct {
}

func (s MemcacheRegistration) InitializeConfig() any {
	return MemcacheConfig{}
}

func (s MemcacheRegistration) Name() string {
	return "memcache"
}

func (s MemcacheRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(MemcacheConfig)

	if config.TTL == 0 {
		config.TTL = memcacheDefaultTTL
	}
	if config.TTL > memcacheMaxTTL {
		config.TTL = memcacheMaxTTL
	}

	if config.Datastore != "" {
		if config.Host != "" || len(config.Servers) > 0 {
			return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "cache.memcache.datastore", "cache.memcache connection parameters")
		}

		ds, ok := deps.Datastores.Get(config.Datastore)
		if !ok {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.memcache.datastore", config.Datastore)
		}

		client, ok := ds.Native().(*memcache.Client)
		if !ok {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.memcache.datastore", config.Datastore)
		}

		return &Memcache{config, client, false}, nil
	}

	if len(config.Servers) == 0 {
		if config.Host == "" {
			config.Host = memcacheDefaultHost
		}
		if config.Port == 0 {
			config.Port = memcacheDefaultPort
		}

		config.Servers = []HostAndPort{{config.Host, config.Port}}
	} else if config.Host != "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "config.memcache.host", "config.memcache.servers")
	}

	addrs := HostAndPortArrayToStringArray(config.Servers)
	mc := memcache.New(addrs...)

	err := mc.Ping()

	return &Memcache{config, mc, true}, err

}

// memcacheKey sanitizes LayerName (see safeLayerName) since memcache keys can't contain whitespace
// or control characters, then bounds the total length, which KeyPrefix counts toward.
func memcacheKey(prefix string, t pkg.TileRequest) string {
	safe := t
	safe.LayerName = safeLayerName(t.LayerName)
	return safeMemcacheKey(prefix, safe.String())
}

// Close shuts down the memcache client, releasing its connection pool. Left alone when the client
// came from a shared datastore, since the datastore registry owns closing it in that case.
func (c Memcache) Close(_ context.Context) error {
	if c.client == nil || !c.ownsClient {
		return nil
	}

	return c.client.Close()
}

func (c Memcache) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	it, err := c.client.Get(memcacheKey(c.KeyPrefix, t))

	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return pkg.DecodeImage(it.Value)
}

func (c Memcache) Save(_ context.Context, t pkg.TileRequest, img *pkg.Image) error {
	val, err := img.Encode()

	if err != nil {
		return err
	}

	return c.client.Set(&memcache.Item{Key: memcacheKey(c.KeyPrefix, t), Value: val, Expiration: int32(c.TTL)}) // #nosec G115 -- max value applied in Initialize
}
