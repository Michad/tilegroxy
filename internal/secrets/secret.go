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

package secrets

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/mitchellh/mapstructure"
)

func ConstructSecreter[C any](rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (entities.Secreter, error) {
	rawConfig = pkg.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := entities.Registration[entities.Secreter](name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			return reg.Initialize(cfg, errorMessages)
		}
	}

	// if rawConfig["name"] == "awssecretsmanager" {
	// 	var config AWSSecretsManagerConfig
	// 	err := mapstructure.Decode(rawConfig, &config)
	// 	if err != nil {
	// 		return nil, err
	// 	}

	// 	return ConstructAWSSecretsManagerConfig(config, errorMessages)
	// }

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.name", nameCoerce)
}
