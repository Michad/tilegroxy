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

	"github.com/Michad/tilegroxy/internal/deprecation"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	memcachedDefaultHost = "127.0.0.1"
	memcachedDefaultPort = 11211
	memcachedDefaultTTL  = 60 * 60 * 24
	memcachedMaxTTL      = 30 * 60 * 60 * 24
)

// Inline connection info is intentionally undocumented, kept functional for compatibility
type MemcachedConfig struct {
	HostAndPort `mapstructure:",squash"`
	Servers     []HostAndPort // The list of servers to use.
	KeyPrefix   string        // Prefix to keynames stored in cache
	TTL         uint          // Cache expiration in seconds. Max of 30 days. Default to 1 day
	Datastore   string        // ID of a memcached datastore to reuse instead of connecting directly. Mutually exclusive with the connection parameters above
}

type Memcached struct {
	MemcachedConfig
	client *memcache.Client
	// False when the client came from a shared datastore, since the registry owns closing it
	ownsClient bool
}

func init() {
	cache.RegisterCache(MemcachedRegistration{})
	cache.RegisterCache(MemcachedLegacyRegistration{})
}

type MemcachedRegistration struct {
}

func (s MemcachedRegistration) InitializeConfig() any {
	return MemcachedConfig{}
}

func (s MemcachedRegistration) Name() string {
	return "memcached"
}

// Deprecated "memcache" alias for MemcachedRegistration
type MemcachedLegacyRegistration struct {
	MemcachedRegistration
}

func (s MemcachedLegacyRegistration) Name() string {
	return "memcache"
}

func (s MemcachedLegacyRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	deprecation.WarnConfig(`cache name "memcache"`, `"memcached"`, deprecation.NextMajorVersion)
	return s.MemcachedRegistration.Initialize(configAny, deps)
}

func (s MemcachedRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(MemcachedConfig)

	if config.TTL == 0 {
		config.TTL = memcachedDefaultTTL
	}
	if config.TTL > memcachedMaxTTL {
		config.TTL = memcachedMaxTTL
	}

	if config.Datastore != "" {
		if config.Host != "" || len(config.Servers) > 0 {
			return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "cache.memcached.datastore", "cache.memcached connection parameters")
		}

		ds, ok := deps.Datastores.Get(config.Datastore)
		if !ok {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.memcached.datastore", config.Datastore)
		}

		client, ok := ds.Native().(*memcache.Client)
		if !ok {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.memcached.datastore", config.Datastore)
		}

		return &Memcached{config, client, false}, nil
	}

	if len(config.Servers) == 0 {
		if config.Host == "" {
			config.Host = memcachedDefaultHost
		}
		if config.Port == 0 {
			config.Port = memcachedDefaultPort
		}

		config.Servers = []HostAndPort{{config.Host, config.Port}}
	} else if config.Host != "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "config.memcached.host", "config.memcached.servers")
	}

	addrs := HostAndPortArrayToStringArray(config.Servers)
	mc := memcache.New(addrs...)

	err := mc.Ping()

	return &Memcached{config, mc, true}, err

}

// Memcached keys can't contain whitespace or control characters, and KeyPrefix counts toward the length limit
func memcachedKey(prefix string, t pkg.TileRequest) string {
	safe := t
	safe.LayerName = safeLayerName(t.LayerName)
	return safeMemcachedKey(prefix, safe.String())
}

// Leaves a client from a shared datastore alone, since the registry owns closing it
func (c Memcached) Close(_ context.Context) error {
	if c.client == nil || !c.ownsClient {
		return nil
	}

	return c.client.Close()
}

func (c Memcached) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	it, err := c.client.Get(memcachedKey(c.KeyPrefix, t))

	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return pkg.DecodeImage(it.Value)
}

func (c Memcached) Save(_ context.Context, t pkg.TileRequest, img *pkg.Image) error {
	val, err := img.Encode()

	if err != nil {
		return err
	}

	return c.client.Set(&memcache.Item{Key: memcachedKey(c.KeyPrefix, t), Value: val, Expiration: int32(c.TTL)}) // #nosec G115 -- max value applied in Initialize
}

func (c Memcached) Remove(_ context.Context, t pkg.TileRequest) (bool, error) {
	err := c.client.Delete(memcachedKey(c.KeyPrefix, t))

	if errors.Is(err, memcache.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}
