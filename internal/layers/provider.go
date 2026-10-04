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

package layers

import (
	"fmt"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

func ConstructProvider(rawConfig map[string]interface{}, deps layer.ProviderDeps) (layer.Provider, error) {
	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := layer.RegisteredProvider(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := configload.DecodeEntityConfig(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			provider, err := reg.Initialize(cfg, deps)

			if err != nil {
				return nil, err
			}

			return ProviderWrapper{Name: name, Provider: provider, dataType: reg.DataType(cfg)}, nil
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "provider.name", nameCoerce, layer.RegisteredProviderNames())
}
