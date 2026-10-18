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

package sample

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
)

type AuthenticationConfig struct {
	Header string
	Key    string // Typically a secret.* reference so the key stays out of the file
	UserID string // Recorded as the user for every request that passes
}

// Accepts requests carrying a fixed key in a header
type Authentication struct {
	AuthenticationConfig
}

func init() {
	authentication.RegisterAuthentication(AuthenticationRegistration{})
}

type AuthenticationRegistration struct{}

func (AuthenticationRegistration) InitializeConfig() any {
	return AuthenticationConfig{Header: "X-Api-Key", UserID: "sample"}
}

func (AuthenticationRegistration) Name() string {
	return "sample"
}

func (AuthenticationRegistration) Initialize(cfgAny any, deps authentication.AuthenticationDeps) (authentication.Authentication, error) {
	cfg := cfgAny.(AuthenticationConfig)

	if cfg.Key == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "authentication.key")
	}

	return &Authentication{cfg}, nil
}

func (a *Authentication) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	if subtle.ConstantTimeCompare([]byte(req.Header.Get(a.Header)), []byte(a.Key)) != 1 {
		return false
	}

	if userID, ok := pkg.UserIDFromContext(ctx); ok && userID != nil {
		*userID = a.UserID
	}

	return true
}
