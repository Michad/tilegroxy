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
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/health"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

func configToEntities(cfg config.Config) (*entities.Entities, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.Secret = pkg.ReplaceEnv(cfg.Secret)
	secreter, err := secret.ConstructSecreter(cfg.Secret, secret.SecreterDeps{ErrorMessages: cfg.Error.Messages})
	if err != nil {
		return nil, fmt.Errorf("error constructing secret: %w", err)
	}

	datastores, err := datastore.ConstructDatastoreRegistry(cfg.Datastores, secreter, cfg.Error.Messages)
	if err != nil {
		return nil, fmt.Errorf("error constructing datastores: %w", err)
	}

	cfg.Cache = pkg.ReplaceEnv(cfg.Cache)
	cfg.Cache, err = pkg.ReplaceConfigValues(cfg.Cache, "secret", secreter.Lookup)
	if err != nil {
		return nil, err
	}

	cacheObj, err := cache.ConstructCache(cfg.Cache, cache.CacheDeps{ErrorMessages: cfg.Error.Messages, Datastores: datastores})
	if err != nil {
		return nil, fmt.Errorf("error constructing cache: %w", err)
	}

	cfg.Authentication = pkg.ReplaceEnv(cfg.Authentication)
	cfg.Authentication, err = pkg.ReplaceConfigValues(cfg.Authentication, "secret", secreter.Lookup)
	if err != nil {
		return nil, err
	}

	auth, err := authentication.ConstructAuth(cfg.Authentication, authentication.AuthenticationDeps{ErrorMessages: cfg.Error.Messages})
	if err != nil {
		return nil, fmt.Errorf("error constructing auth: %w", err)
	}

	analyticsObj, err := analytics.ConstructAnalytics(cfg.Analytics, secreter, analytics.AnalyticsDeps{Datastores: datastores, ErrorMessages: cfg.Error.Messages})
	if err != nil {
		return nil, fmt.Errorf("error constructing analytics: %w", err)
	}

	layerGroup, err := layer.ConstructLayerGroup(cfg, cacheObj, secreter, datastores)
	if err != nil {
		return nil, fmt.Errorf("error constructing layers: %w", err)
	}

	// Constructed only to validate their config, then discarded; serve builds its own. Otherwise a
	// bad check name would first surface when serve binds the health port, after `config check`
	// already called the config Valid.
	for _, checkCfg := range cfg.Server.Health.Checks {
		if _, err := health.ConstructHealthCheck(checkCfg, layerGroup, &cfg); err != nil {
			return nil, fmt.Errorf("error constructing health check: %w", err)
		}
	}

	return &entities.Entities{
		LayerGroup: layerGroup,
		Auth:       auth,
		Analytics:  analyticsObj,
		Cache:      cacheObj,
		Datastores: datastores,
	}, nil
}
