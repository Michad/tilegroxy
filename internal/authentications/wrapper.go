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
	"context"
	"fmt"
	"net/http"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/tracing"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

// A struct that wraps all other auths in order to add in instrumentation, specifically child spans for tracing flow. This is used even when telemetry is disabled but OTEL handles no-op'ing in that case so performance impact is minimal
type AuthWrapper struct {
	Name string
	Auth authentication.Authentication
}

func (w AuthWrapper) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	newCtx, span := tracing.MakeChildSpan(ctx, nil, "Authentication", w.Name, "CheckAuthentication")
	defer span.End()

	return w.Auth.CheckAuthentication(newCtx, req)
}

// Close forwards to the wrapped auth when it holds resources needing release. Without this the
// wrapper would hide the underlying auth's Closer implementation from the shutdown path.
func (w AuthWrapper) Close(ctx context.Context) error {
	return lifecycle.CloseIfCloser(ctx, w.Auth)
}

func ConstructAuth(rawConfig map[string]interface{}, deps authentication.AuthenticationDeps) (authentication.Authentication, error) {
	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := authentication.RegisteredAuthentication(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := configload.DecodeEntityConfig(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			a, err := reg.Initialize(cfg, deps)
			return AuthWrapper{Name: name, Auth: a}, err
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "authentication.name", nameCoerce, authentication.RegisteredAuthenticationNames())
}
