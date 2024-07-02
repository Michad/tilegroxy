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
	"strconv"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/mitchellh/mapstructure"
)

type Cache interface {
	Lookup(t internal.TileRequest) (*internal.Image, error)
	Save(t internal.TileRequest, img *internal.Image) error
}

func ConstructCache(rawConfig map[string]interface{}, errorMessages *config.ErrorMessages) (Cache, error) {
	rawConfig = internal.ReplaceEnv(rawConfig)

	if rawConfig["name"] == "none" || rawConfig["name"] == "test" {
		return Noop{}, nil
	} else if rawConfig["name"] == "disk" {
		var config DiskConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructDisk(config, errorMessages)
	} else if rawConfig["name"] == "memory" {
		var config MemoryConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructMemory(config, errorMessages)
	} else if rawConfig["name"] == "multi" {
		var config MultiConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		tierCaches := make([]Cache, len(config.Tiers))

		for i, tierRawConfig := range config.Tiers {
			tierCache, err := ConstructCache(tierRawConfig, errorMessages)

			if err != nil {
				return nil, err
			}

			tierCaches[i] = tierCache
		}

		return Multi{tierCaches}, nil
	} else if rawConfig["name"] == "s3" {
		var config S3Config
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructS3(&config, errorMessages)
	} else if rawConfig["name"] == "redis" {
		var config RedisConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructRedis(&config, errorMessages)
	} else if rawConfig["name"] == "memcache" {
		var config MemcacheConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructMemcache(&config, errorMessages)
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.InvalidParam, "cache.name", name)
}

// Utility type used in a couple caches
type HostAndPort struct {
	Host string
	Port uint16
}

func (hp HostAndPort) String() string {
	return hp.Host + ":" + strconv.Itoa(int(hp.Port))
}

func HostAndPortArrayToStringArray(servers []HostAndPort) []string {
	addrs := make([]string, len(servers))

	for i, addr := range servers {
		addrs[i] = addr.String()
	}

	return addrs
}
