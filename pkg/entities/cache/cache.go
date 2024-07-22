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

package cache

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/mitchellh/mapstructure"
)

type Cache interface {
	Lookup(t pkg.TileRequest) (*pkg.Image, error)
	Save(t pkg.TileRequest, img *pkg.Image) error
}

type CacheRegistration interface {
	Name() string
	Initialize(config any, errorMessages config.ErrorMessages) (Cache, error)
	InitializeConfig() any
}

var registrations map[string]CacheRegistration = make(map[string]CacheRegistration)

func RegisterCache(reg CacheRegistration) {
	registrations[reg.Name()] = reg
}

func RegisteredCache(name string) (CacheRegistration, bool) {
	o, ok := registrations[name]
	return o, ok
}

func RegisteredCacheNames() []string {
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

func ConstructCache(rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (Cache, error) {
	name, ok := rawConfig["name"].(string)

	if ok {
		if name == "test" || name == "Test" {
			name = "none"
		}

		reg, ok := RegisteredCache(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			a, err := reg.Initialize(cfg, errorMessages)
			return a, err
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.EnumError, "cache.name", nameCoerce, RegisteredCacheNames())
}
