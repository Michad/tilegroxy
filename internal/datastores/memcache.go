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
	memcacheDefaultHost = "127.0.0.1"
	memcacheDefaultPort = 11211
)

type MemcacheHostAndPort struct {
	Host string
	Port uint16
}

func (hp MemcacheHostAndPort) String() string {
	return hp.Host + ":" + strconv.Itoa(int(hp.Port))
}

type MemcacheWrapperConfig struct {
	ID      string
	Host    string // Convenience equivalent to supplying Servers with a single entry
	Port    uint16
	Servers []MemcacheHostAndPort
}

type MemcacheWrapper struct {
	MemcacheWrapperConfig
	client *memcache.Client
}

func init() {
	datastore.RegisterDatastoreWrapper(MemcacheWrapperRegistration{})
}

type MemcacheWrapperRegistration struct {
}

func (s MemcacheWrapperRegistration) InitializeConfig() any {
	return MemcacheWrapperConfig{}
}

func (s MemcacheWrapperRegistration) Name() string {
	return "memcache"
}

func (s MemcacheWrapperRegistration) Initialize(cfgAny any, deps datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(MemcacheWrapperConfig)

	if len(cfg.Servers) == 0 {
		if cfg.Host == "" {
			cfg.Host = memcacheDefaultHost
		}
		if cfg.Port == 0 {
			cfg.Port = memcacheDefaultPort
		}

		cfg.Servers = []MemcacheHostAndPort{{cfg.Host, cfg.Port}}
	} else if cfg.Host != "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "datastore.memcache.host", "datastore.memcache.servers")
	}

	addrs := make([]string, len(cfg.Servers))
	for i, addr := range cfg.Servers {
		addrs[i] = addr.String()
	}

	client := memcache.New(addrs...)

	if err := client.Ping(); err != nil {
		return nil, err
	}

	return &MemcacheWrapper{cfg, client}, nil
}

func (w MemcacheWrapper) GetID() string {
	return w.ID
}

func (w MemcacheWrapper) Native() any {
	return w.client
}

// Close shuts down the memcache client, releasing its connection pool.
func (w MemcacheWrapper) Close(_ context.Context) error {
	if w.client == nil {
		return nil
	}

	return w.client.Close()
}
