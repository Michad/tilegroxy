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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/mitchellh/mapstructure"
)

func ConstructCache(rawConfig map[string]interface{}, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (entities.Cache, error) {
	rawConfig = pkg.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		if name == "test" || name == "Test" {
			name = "none"
		}

		reg, ok := entities.Registration[entities.Cache](name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			a, err := reg.Initialize(cfg, clientConfig, errorMessages)
			return a, err
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.EnumError, "cache.name", nameCoerce, entities.RegisteredCacheNames())
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
