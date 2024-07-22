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

package authentication

import (
	"fmt"
	"net/http"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/mitchellh/mapstructure"
)

type Authentication interface {
	CheckAuthentication(req *http.Request, ctx *pkg.RequestContext) bool
}

type AuthenticationRegistration interface {
	Name() string
	Initialize(config any, errorMessages config.ErrorMessages) (Authentication, error)
	InitializeConfig() any
}

var registrations map[string]AuthenticationRegistration = make(map[string]AuthenticationRegistration)

func RegisterAuthentication(reg AuthenticationRegistration) {
	registrations[reg.Name()] = reg
}

func RegisteredAuthentication(name string) (AuthenticationRegistration, bool) {
	o, ok := registrations[name]
	return o, ok
}

func RegisteredAuthenticationNames() []string {
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

func ConstructAuth(rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (Authentication, error) {
	rawConfig = pkg.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := RegisteredAuthentication(name)
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
	return nil, fmt.Errorf(errorMessages.EnumError, "authentication.name", nameCoerce, RegisteredAuthenticationNames())
}
