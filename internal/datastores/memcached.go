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

package datastores

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	memcachedDefaultHost = "127.0.0.1"
	memcachedDefaultPort = 11211
)

type MemcachedHostAndPort struct {
	Host string
	Port uint16
}

func (hp MemcachedHostAndPort) String() string {
	return hp.Host + ":" + strconv.Itoa(int(hp.Port))
}

type MemcachedWrapperConfig struct {
	ID      string
	Host    string // Convenience equivalent to supplying Servers with a single entry
	Port    uint16
	Servers []MemcachedHostAndPort
}

type MemcachedWrapper struct {
	MemcachedWrapperConfig
	client *memcache.Client
}

func init() {
	datastore.RegisterDatastoreWrapper(MemcachedWrapperRegistration{})
	datastore.RegisterDatastoreWrapper(MemcachedWrapperLegacyRegistration{})
}

type MemcachedWrapperRegistration struct {
}

func (s MemcachedWrapperRegistration) InitializeConfig() any {
	return MemcachedWrapperConfig{}
}

func (s MemcachedWrapperRegistration) Name() string {
	return "memcached"
}

// MemcachedWrapperLegacyRegistration is the deprecated "memcache" alias for MemcachedWrapperRegistration
type MemcachedWrapperLegacyRegistration struct {
	MemcachedWrapperRegistration
}

func (s MemcachedWrapperLegacyRegistration) Name() string {
	return "memcache"
}

func (s MemcachedWrapperRegistration) Initialize(cfgAny any, deps datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(MemcachedWrapperConfig)

	if len(cfg.Servers) == 0 {
		if cfg.Host == "" {
			cfg.Host = memcachedDefaultHost
		}
		if cfg.Port == 0 {
			cfg.Port = memcachedDefaultPort
		}

		cfg.Servers = []MemcachedHostAndPort{{cfg.Host, cfg.Port}}
	} else if cfg.Host != "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "datastore.memcached.host", "datastore.memcached.servers")
	}

	addrs := make([]string, len(cfg.Servers))
	for i, addr := range cfg.Servers {
		addrs[i] = addr.String()
	}

	client := memcache.New(addrs...)

	if err := client.Ping(); err != nil {
		return nil, err
	}

	return &MemcachedWrapper{cfg, client}, nil
}

func (w MemcachedWrapper) GetID() string {
	return w.ID
}

func (w MemcachedWrapper) Native() any {
	return w.client
}

// Close shuts down the memcached client, releasing its connection pool.
func (w MemcachedWrapper) Close(_ context.Context) error {
	if w.client == nil {
		return nil
	}

	return w.client.Close()
}
