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

package secret

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/mitchellh/mapstructure"
)

type Secreter interface {
	Lookup(key string) (string, error)
}

type SecreterRegistration interface {
	Name() string
	Initialize(config any, errorMessages config.ErrorMessages) (Secreter, error)
	InitializeConfig() any
}

var registrations = make(map[string]SecreterRegistration)

func RegisterSecreter(reg SecreterRegistration) {
	registrations[reg.Name()] = reg
}

func RegisteredSecreter(name string) (SecreterRegistration, bool) {
	o, ok := registrations[name]
	return o, ok
}

func RegisteredSecreterNames() []string {
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

func ConstructSecreter(rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (Secreter, error) {
	rawConfig = pkg.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := RegisteredSecreter(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			return reg.Initialize(cfg, errorMessages)
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.EnumError, "secret.name", nameCoerce, RegisteredSecreterNames())
}
