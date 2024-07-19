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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/mitchellh/mapstructure"
)

type Secreter[C any] interface {
	Lookup(ctx *internal.RequestContext, key string) (string, error)
}

func ConstructSecreter[C any](rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (*Secreter[C], error) {
	rawConfig = internal.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		regAny, ok := pkg.Registration[C, Secreter[C]](pkg.EntitySecret, name)
		if ok {
			reg, ok := regAny.(pkg.EntityRegistration[C, Secreter[C]])
			if ok {
				cfg := reg.InitializeConfig()
				err := mapstructure.Decode(rawConfig, &cfg)
				if err != nil {
					return nil, err
				}
				return reg.Initialize(cfg, errorMessages)
			}
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
