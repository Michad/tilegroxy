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

package entry

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	internalanalytics "github.com/Michad/tilegroxy/internal/analytics"

	"github.com/Michad/tilegroxy/internal/authentications"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/checks"
	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/Michad/tilegroxy/internal/secrets"
	"github.com/Michad/tilegroxy/internal/tlsprofiles"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	pkgratelimit "github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

func configToEntities(ctx context.Context, cfg config.Config, reloadFunc func(reason string)) (*entities.Entities, error) {
	if err := configload.Validate(cfg); err != nil {
		return nil, err
	}

	built := &entities.Entities{}

	cfg.Secret = configload.ReplaceEnv(cfg.Secret)
	secreter, err := secrets.ConstructSecreter(cfg.Secret, secret.SecreterDeps{ErrorMessages: cfg.Error.Messages, ReloadFunc: reloadFunc})
	if err != nil {
		return nil, fmt.Errorf("error constructing secret: %w", err)
	}
	built.Secreter = secreter

	built.TLS, err = tlsprofiles.Construct(ctx, cfg.TLS, secreter, cfg.Error.Messages)
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing tls: %w", err))
	}

	transport, err := applyTLSProfiles(&cfg, built.TLS)
	if err != nil {
		return nil, closeAndReturn(built, err)
	}

	datastores, err := datastores.ConstructDatastoreRegistry(ctx, cfg.Datastores, secreter, cfg.Error.Messages)
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing datastores: %w", err))
	}
	built.Datastores = datastores

	caches, err := caches.ConstructCacheRegistry(ctx, cfg.Cache, cfg.DefaultCache, secreter, cache.CacheDeps{ErrorMessages: cfg.Error.Messages, Datastores: datastores})
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing cache: %w", err))
	}
	built.Caches = caches

	cfg.Authentication = configload.ReplaceEnv(cfg.Authentication)
	cfg.Authentication, err = configload.ReplaceConfigValues(cfg.Authentication, "secret", func(k string) (string, error) {
		v, _, lookupErr := secreter.Lookup(ctx, k)
		return v, lookupErr
	})
	if err != nil {
		return nil, closeAndReturn(built, err)
	}

	auth, err := authentications.ConstructAuth(cfg.Authentication, authentication.AuthenticationDeps{ErrorMessages: cfg.Error.Messages, ClientConfig: cfg.Client, Transport: transport, Datastores: datastores, ClientTransport: built.TLS.ClientTransport})
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing auth: %w", err))
	}
	built.Auth = auth

	analyticsObj, err := internalanalytics.ConstructAnalytics(ctx, cfg.Analytics, secreter, analytics.AnalyticsDeps{Datastores: datastores, ErrorMessages: cfg.Error.Messages})
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing analytics: %w", err))
	}
	built.Analytics = analyticsObj

	limiter, err := ratelimit.ConstructLimiter(ctx, cfg, secreter, pkgratelimit.RateLimiterDeps{Datastores: datastores, ErrorMessages: cfg.Error.Messages})
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing ratelimit: %w", err))
	}
	built.RateLimit = limiter

	layerGroup, err := layers.ConstructLayerGroup(ctx, cfg, caches, secreter, datastores, built.TLS.ClientTransport)
	if err != nil {
		return nil, closeAndReturn(built, fmt.Errorf("error constructing layers: %w", err))
	}
	built.LayerGroup = layerGroup

	// Built only to validate, then discarded. Otherwise a bad check would first fail when serve binds the health port
	for _, checkCfg := range cfg.Health.Checks {
		if _, err := checks.ConstructHealthCheck(checkCfg, layerGroup, caches, &cfg); err != nil {
			return nil, closeAndReturn(built, fmt.Errorf("error constructing health check: %w", err))
		}
	}

	return built, nil
}

// Rebuilds layer clients rather than editing them, since cfg shares them with the live config a later reload re-resolves.
// Layer references are resolved here so a bad one fails before datastores connect.
func applyTLSProfiles(cfg *config.Config, reg *tlsprofiles.Registry) (http.RoundTripper, error) {
	// Pinned so a layer overriding only tls keeps presenting the top-level certificate
	if cfg.Client.MTLS == "" && cfg.Client.EnableMTLS != nil && *cfg.Client.EnableMTLS {
		cfg.Client.MTLS = cfg.Client.TLS
	}

	transport, err := reg.ClientTransport(cfg.Client)
	if err != nil {
		return nil, err
	}

	cfg.Layers = slices.Clone(cfg.Layers)

	for i := range cfg.Layers {
		if cfg.Layers[i].Client == nil {
			continue
		}

		client := *cfg.Layers[i].Client
		configload.MergeClientDefaults(&client, cfg.Client)

		if _, err = reg.ClientTransport(client); err != nil {
			return nil, fmt.Errorf("error constructing layers[%d].client: %w", i, err)
		}

		cfg.Layers[i].Client = &client
	}

	return transport, nil
}

// Releases everything built before a construction failure, joining any close error into err
func closeAndReturn(built *entities.Entities, err error) error {
	if closeErr := built.Close(context.Background()); closeErr != nil {
		return fmt.Errorf("%w (additionally failed to release already-constructed entities: %w)", err, closeErr)
	}

	return err
}
