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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/mitchellh/mapstructure"
)

type Authentication interface {
	CheckAuthentication(req *http.Request, ctx *internal.RequestContext) bool
}

func ConstructAuth(rawConfig map[string]interface{}, errorMessages *config.ErrorMessages) (Authentication, error) {
	if rawConfig["name"] == "none" {
		return Noop{}, nil
	} else if rawConfig["name"] == "static key" {
		var config StaticKeyConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructStaticKey(&config, errorMessages)
	} else if rawConfig["name"] == "jwt" {
		var config JwtConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructJwt(&config, errorMessages)
	} else if rawConfig["name"] == "custom" {
		var config CustomConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructCustom(&config, errorMessages)
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.name", name)
}
