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

package tg

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

func configToEntities(cfg config.Config) (*layer.LayerGroup, authentication.Authentication, error) {
	cfg.Secret = pkg.ReplaceEnv(cfg.Secret)
	secreter, err := secret.ConstructSecreter(cfg.Secret, cfg.Error.Messages)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing secret: %w", err)
	}

	cfg.Cache = pkg.ReplaceEnv(cfg.Cache)
	cfg.Cache, err = pkg.ReplaceConfigValues(cfg.Cache, "secret", secreter.Lookup)
	if err != nil {
		return nil, nil, err
	}

	cache, err := cache.ConstructCache(cfg.Cache, cfg.Error.Messages)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing cache: %w", err)
	}

	cfg.Authentication = pkg.ReplaceEnv(cfg.Authentication)
	cfg.Authentication, err = pkg.ReplaceConfigValues(cfg.Authentication, "secret", secreter.Lookup)
	if err != nil {
		return nil, nil, err
	}

	auth, err := authentication.ConstructAuth(cfg.Authentication, cfg.Error.Messages)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing auth: %w", err)
	}

	layerGroup, err := layer.ConstructLayerGroup(cfg, cache, secreter)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing layers: %w", err)
	}

	return layerGroup, auth, err
}
