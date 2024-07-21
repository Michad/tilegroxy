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

package authentications

import (
	"net/http"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
)

type NoopConfig struct {
}

type Noop struct {
	NoopConfig
}

func init() {
	authentication.RegisterAuthentication(NoopRegistration{})
}

type NoopRegistration struct {
}

func (s NoopRegistration) InitializeConfig() any {
	return NoopConfig{}
}

func (s NoopRegistration) Name() string {
	return "none"
}

func (s NoopRegistration) Initialize(config any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (authentication.Authentication, error) {
	return &Noop{config.(NoopConfig)}, nil
}

func (c Noop) CheckAuthentication(req *http.Request, ctx *pkg.RequestContext) bool {
	return true
}
