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
	"fmt"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/bradfitz/gomemcache/memcache"
)

type MemcachedConfig struct {
	HostAndPort `mapstructure:",squash"`
	Servers     []HostAndPort //The list of servers to use.
	KeyPrefix   string        //Prefix to keynames stored in cache
	Ttl         uint32        //Cache expiration in seconds. Max of 30 days. Default to 1 day
}

const (
	memcachedDefaultHost = "127.0.0.1"
	memcachedDefaultPort = 11211
	memcachedDefaultTtl  = 60 * 60 * 24
	memcachedMaxTtl      = 30 * 60 * 60 * 24
)

type Memcached struct {
	*MemcachedConfig
	client *memcache.Client
}

func ConstructMemcached(config *MemcachedConfig, errorMessages *config.ErrorMessages) (*Memcached, error) {
	if config.Servers == nil || len(config.Servers) == 0 {
		if config.Host == "" {
			config.Host = memcachedDefaultHost
		}
		if config.Port == 0 {
			config.Port = memcachedDefaultPort
		}

		config.Servers = []HostAndPort{{config.Host, config.Port}}
	} else {
		if config.Host != "" {
			return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "config.memcached.host", "config.memcached.servers")
		}
	}

	if config.Ttl == 0 {
		config.Ttl = memcachedDefaultTtl
	}
	if config.Ttl > memcachedMaxTtl {
		config.Ttl = memcachedMaxTtl
	}

	addrs := HostAndPortArrayToStringArray(config.Servers)
	mc := memcache.New(addrs...)

	err := mc.Ping()

	return &Memcached{config, mc}, err

}

func (c Memcached) Lookup(t internal.TileRequest) (*internal.Image, error) {
	it, err := c.client.Get(c.KeyPrefix + t.String())

	if err != nil {
		return nil, err
	}

	result := internal.Image(it.Value)

	return &result, nil
}

func (c Memcached) Save(t internal.TileRequest, img *internal.Image) error {
	return c.client.Set(&memcache.Item{Key: c.KeyPrefix + t.String(), Value: *img, Expiration: int32(c.Ttl)})
}
