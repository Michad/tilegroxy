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

package checks

import (
	"fmt"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/health"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

func ConstructHealthCheck(rawConfig map[string]interface{}, lg layer.LayerGroup, caches cache.CacheRegistry, allCfg *config.Config) (health.HealthCheck, error) {
	rawConfig = configload.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := health.RegisteredHealthCheck(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := configload.DecodeEntityConfig(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			return reg.Initialize(cfg, health.HealthCheckDeps{LayerGroup: lg, Caches: caches, AllConfig: allCfg})
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(allCfg.Error.Messages.EnumError, "check.name", nameCoerce, health.RegisteredHealthCheckNames())
}
