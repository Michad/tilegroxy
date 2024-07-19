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

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/mitchellh/mapstructure"
)

type Secreter interface {
	Lookup(key string) (string, error)
}

func ConstructSecreter(rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (Secreter, error) {
	if rawConfig["name"] == "none" {
		var config NoopConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}

		return ConstructNoopConfig(config, errorMessages)
	} else if rawConfig["name"] == "awssecretsmanager" {
		var config AWSSecretsManagerConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}

		return ConstructAWSSecretsManagerConfig(config, errorMessages)
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.name", name)
}
