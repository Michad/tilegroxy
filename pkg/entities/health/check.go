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

package health

import (
	"context"
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/mitchellh/mapstructure"
)

type HealthCheck interface {
	Check(ctx context.Context) error
	GetDelay() uint
}

type HealthCheckConfig interface {
	GetDelay() uint
}

type HealthCheckRegistration interface {
	Name() string
	Initialize(checkConfig HealthCheckConfig, lg *layer.LayerGroup, cache cache.Cache, allCfg *config.Config) (HealthCheck, error)
	InitializeConfig() HealthCheckConfig
}

var registrations = make(map[string]HealthCheckRegistration)

func RegisterHealthCheck(reg HealthCheckRegistration) {
	registrations[reg.Name()] = reg
}

func RegisteredHealthCheck(name string) (HealthCheckRegistration, bool) {
	o, ok := registrations[name]
	return o, ok
}

func RegisteredHealthCheckNames() []string {
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

func ConstructHealthCheck(rawConfig map[string]interface{}, lg *layer.LayerGroup, allCfg *config.Config) (HealthCheck, error) {
	rawConfig = pkg.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := RegisteredHealthCheck(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			return reg.Initialize(cfg, lg, lg.DefaultCache, allCfg)
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(allCfg.Error.Messages.EnumError, "check.name", nameCoerce, RegisteredHealthCheckNames())
}
