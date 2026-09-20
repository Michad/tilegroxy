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
	"context"
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

func configToEntities(ctx context.Context, cfg config.Config, reloadFunc func()) (*entities.Entities, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	built := &entities.Entities{}

	cfg.Secret = pkg.ReplaceEnv(cfg.Secret)
	secreter, err := secret.ConstructSecreter(cfg.Secret, secret.SecreterDeps{ErrorMessages: cfg.Error.Messages, ReloadFunc: reloadFunc})
	if err != nil {
		return nil, fmt.Errorf("error constructing secret: %w", err)
	}
	built.Secreter = secreter

	datastores, err := datastore.ConstructDatastoreRegistry(ctx, cfg.Datastores, secreter, cfg.Error.Messages)
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing datastores: %w", err))
	}
	built.Datastores = datastores

	caches, err := cache.ConstructCacheRegistry(ctx, cfg.Cache, cfg.DefaultCache, secreter, cache.CacheDeps{ErrorMessages: cfg.Error.Messages, Datastores: datastores})
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing cache: %w", err))
	}
	built.Caches = caches

	cfg.Authentication = pkg.ReplaceEnv(cfg.Authentication)
	cfg.Authentication, err = pkg.ReplaceConfigValues(cfg.Authentication, "secret", func(k string) (string, error) {
		v, _, lookupErr := secreter.Lookup(ctx, k)
		return v, lookupErr
	})
	if err != nil {
		return nil, closeAndReturn(built, err)
	}

	auth, err := authentication.ConstructAuth(cfg.Authentication, authentication.AuthenticationDeps{ErrorMessages: cfg.Error.Messages})
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing auth: %w", err))
	}
	built.Auth = auth

	analyticsObj, err := analytics.ConstructAnalytics(ctx, cfg.Analytics, secreter, analytics.AnalyticsDeps{Datastores: datastores, ErrorMessages: cfg.Error.Messages})
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing analytics: %w", err))
	}
	built.Analytics = analyticsObj

	layerGroup, err := layer.ConstructLayerGroup(ctx, cfg, caches, secreter, datastores)
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing layers: %w", err))
	}
	built.LayerGroup = layerGroup

	// Constructed only to validate their config, then discarded; serve builds its own. Otherwise a
	// bad check name would first surface when serve binds the health port, after `config check`
	// already called the config Valid.
	for _, checkCfg := range cfg.Health.Checks {
		if _, err := health.ConstructHealthCheck(checkCfg, layerGroup, caches, &cfg); err != nil {
			return nil, closeAndReturn(built, fmt.Errorf("error constructing health check: %w", err))
		}
	}

	return built, nil
}

// closeAndReturn releases every entity built before a construction failure and folds any error from
// doing so into the one that's already failing the build.
func closeAndReturn(built *entities.Entities, err error) error {
	if closeErr := built.Close(context.Background()); closeErr != nil {
		return fmt.Errorf("%w (additionally failed to release already-constructed entities: %w)", err, closeErr)
	}

	return err
}
