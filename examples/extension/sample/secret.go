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
	"fmt"

	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// SecretConfig holds the secrets inline. A real secret source would fetch them from a vault instead
type SecretConfig struct {
	Values map[string]string
}

type Secret struct {
	SecretConfig
}

func init() {
	secret.RegisterSecreter(SecretRegistration{})
}

type SecretRegistration struct{}

func (SecretRegistration) InitializeConfig() any {
	return SecretConfig{}
}

func (SecretRegistration) Name() string {
	return "sample"
}

// Zero means this source can't report changes, so secret watching can't be enabled for it
func (SecretRegistration) CheckBatchSize() int {
	return 0
}

func (SecretRegistration) Initialize(cfgAny any, deps secret.SecreterDeps) (secret.Secreter, error) {
	cfg := cfgAny.(SecretConfig)

	if len(cfg.Values) == 0 {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "secret.values")
	}

	return &Secret{cfg}, nil
}

func (s *Secret) Lookup(_ context.Context, key string) (string, string, error) {
	value, ok := s.Values[key]
	if !ok {
		return "", "", fmt.Errorf("no secret named %q", key)
	}

	return value, "", nil
}

func (s *Secret) Check(_ context.Context, keys []string) ([]string, error) {
	return make([]string, len(keys)), nil
}
