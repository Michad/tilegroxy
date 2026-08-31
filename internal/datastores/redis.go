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
	"slices"
	"strconv"

	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/redis/go-redis/v9"
)

const (
	RedisModeStandalone = "standalone"
	RedisModeCluster    = "cluster"
	RedisModeRing       = "ring"
)

var AllRedisModes = []string{RedisModeStandalone, RedisModeCluster, RedisModeRing}

type RedisHostAndPort struct {
	Host string
	Port uint16
}

func (hp RedisHostAndPort) String() string {
	return hp.Host + ":" + strconv.Itoa(int(hp.Port))
}

const (
	redisDefaultHost = "127.0.0.1"
	redisDefaultPort = 6379
)

type RedisWrapperConfig struct {
	ID       string
	Host     string // Convenience equivalent to supplying Servers with a single entry
	Port     uint16
	DB       int    // Database number, defaults to 0. Unused in cluster mode
	Username string // Username to use to authenticate
	Password string // Password to use to authenticate
	Mode     string // Controls operating mode. One of AllRedisModes. Defaults to standalone
	Servers  []RedisHostAndPort
}

type RedisWrapper struct {
	RedisWrapperConfig
	client redis.UniversalClient
}

func init() {
	datastore.RegisterDatastoreWrapper(RedisWrapperRegistration{})
}

type RedisWrapperRegistration struct {
}

func (s RedisWrapperRegistration) InitializeConfig() any {
	return RedisWrapperConfig{}
}

func (s RedisWrapperRegistration) Name() string {
	return "redis"
}

func (s RedisWrapperRegistration) Initialize(cfgAny any, deps datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(RedisWrapperConfig)

	if cfg.Mode == "" {
		cfg.Mode = RedisModeStandalone
	}

	if !slices.Contains(AllRedisModes, cfg.Mode) {
		return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "datastore.redis.mode", cfg.Mode, AllRedisModes)
	}

	if len(cfg.Servers) == 0 {
		if cfg.Host == "" {
			cfg.Host = redisDefaultHost
		}
		if cfg.Port == 0 {
			cfg.Port = redisDefaultPort
		}

		cfg.Servers = []RedisHostAndPort{{cfg.Host, cfg.Port}}
	} else if cfg.Host != "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "datastore.redis.host", "datastore.redis.servers")
	}

	var client redis.UniversalClient

	switch cfg.Mode {
	case RedisModeCluster:
		if cfg.DB != 0 {
			return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "datastore.redis.db", "datastore.redis.cluster")
		}

		addrs := make([]string, len(cfg.Servers))
		for i, addr := range cfg.Servers {
			addrs[i] = addr.String()
		}

		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    addrs,
			Username: cfg.Username,
			Password: cfg.Password,
		})
	case RedisModeRing:
		if len(cfg.Servers) < 2 {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "length(datastore.redis.servers)", len(cfg.Servers))
		}

		addrMap := make(map[string]string)
		for _, addr := range cfg.Servers {
			addrMap[addr.Host] = ":" + strconv.Itoa(int(addr.Port))
		}

		client = redis.NewRing(&redis.RingOptions{
			Addrs:    addrMap,
			Username: cfg.Username,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
	default:
		client = redis.NewClient(&redis.Options{
			Addr:     cfg.Servers[0].String(),
			Username: cfg.Username,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
	}

	return &RedisWrapper{cfg, client}, nil
}

func (w RedisWrapper) GetID() string {
	return w.ID
}

func (w RedisWrapper) Native() any {
	return w.client
}

// Close shuts down the underlying redis client, releasing its connection pool.
func (w RedisWrapper) Close(_ context.Context) error {
	if w.client == nil {
		return nil
	}

	return w.client.Close()
}
