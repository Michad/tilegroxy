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
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	memcacheDefaultHost = "127.0.0.1"
	memcacheDefaultPort = 11211
	memcacheDefaultTTL  = 60 * 60 * 24
	memcacheMaxTTL      = 30 * 60 * 60 * 24
)

type MemcacheConfig struct {
	HostAndPort `mapstructure:",squash"`
	Servers     []HostAndPort // The list of servers to use.
	KeyPrefix   string        // Prefix to keynames stored in cache
	TTL         uint          // Cache expiration in seconds. Max of 30 days. Default to 1 day
}

type Memcache struct {
	MemcacheConfig
	client *memcache.Client
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

func (s MemcacheRegistration) Initialize(configAny any, errorMessages config.ErrorMessages) (cache.Cache, error) {
	config := configAny.(MemcacheConfig)

	if len(config.Servers) == 0 {
		if config.Host == "" {
			config.Host = memcacheDefaultHost
		}
		if config.Port == 0 {
			config.Port = memcacheDefaultPort
		}

		config.Servers = []HostAndPort{{config.Host, config.Port}}
	} else if config.Host != "" {
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "config.memcache.host", "config.memcache.servers")
	}

	if config.TTL == 0 {
		config.TTL = memcacheDefaultTTL
	}
	if config.TTL > memcacheMaxTTL {
		config.TTL = memcacheMaxTTL
	}

	addrs := HostAndPortArrayToStringArray(config.Servers)
	mc := memcache.New(addrs...)

	err := mc.Ping()

	return &Memcache{config, mc}, err

}

func (c Memcache) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	it, err := c.client.Get(c.KeyPrefix + t.String())

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

	return c.client.Set(&memcache.Item{Key: c.KeyPrefix + t.String(), Value: val, Expiration: int32(c.TTL)}) // #nosec G115 -- max value applied in Initialize
}
