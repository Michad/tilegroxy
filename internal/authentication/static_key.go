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
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
)

type StaticKeyConfig struct {
	Key string
}

type StaticKey struct {
	StaticKeyConfig
}

func init() {
	pkg.Register(pkg.EntityAuth, StaticKeyRegistration{})
}

type StaticKeyRegistration struct {
}

func (s StaticKeyRegistration) InitializeConfig() any {
	return StaticKeyConfig{}
}

func (s StaticKeyRegistration) Name() string {
	return "static key"
}

func (s StaticKeyRegistration) Initialize(cfgAny any, errorMessages config.ErrorMessages) (Authentication, error) {
	cfg := cfgAny.(StaticKeyConfig)
	if cfg.Key == "" {
		keyStr := internal.RandomString()

		slog.WarnContext(context.Background(), fmt.Sprintf("Generated authentication key: %v\n", keyStr))
		cfg.Key = keyStr
	}

	return &StaticKey{cfg}, nil
}

func (c StaticKey) CheckAuthentication(req *http.Request, ctx *internal.RequestContext) bool {
	h := req.Header["Authorization"]
	return len(h) > 0 && h[0] == "Bearer "+c.Key
}
