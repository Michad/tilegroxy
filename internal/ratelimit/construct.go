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

package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// Default, so an absent ratelimit block behaves the same as an explicitly disabled one
const noneName = "none"

const (
	defaultTimeout    = 100 * time.Millisecond
	defaultIPv6Prefix = 64
	maxIPv6Prefix     = 128
)

// Accepted by every store, so they're split off before the store decodes the rest
type CommonConfig struct {
	Policies   []PolicyConfig
	Headers    string
	Timeout    uint // In milliseconds
	OnError    string
	IPv6Prefix *uint
}

var commonKeys = []string{"policies", "headers", "timeout", "onerror", "ipv6prefix"}

// Splits the common options from the store's own
func splitConfig(raw map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	common := map[string]interface{}{}
	store := map[string]interface{}{}

	for k, v := range raw {
		if slices.Contains(commonKeys, strings.ToLower(k)) {
			common[k] = v
		} else {
			store[k] = v
		}
	}

	return common, store
}

// Exposed for the ratelimit command, which only needs the store's own datastore
func StoreDatastoreID(rawConfig map[string]interface{}) string {
	for k, v := range configload.ReplaceEnv(rawConfig) {
		if strings.EqualFold(k, "datastore") {
			id, _ := v.(string)
			return id
		}
	}

	return ""
}

// Validates the whole rate limit configuration, including the policies layers refer to.
// secreter is separate from deps because it resolves the raw config before the store is constructed
func ConstructLimiter(ctx context.Context, cfg config.Config, secreter secret.Secreter, deps ratelimit.RateLimiterDeps) (*Limiter, error) {
	l, reg, storeRaw, err := compileLimiter(ctx, cfg, secreter, deps.ErrorMessages)
	if err != nil {
		return nil, err
	}

	storeCfg := reg.InitializeConfig()
	if err := configload.DecodeEntityConfig(storeRaw, &storeCfg); err != nil {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "ratelimit."+l.storeName, err.Error())
	}

	l.store, err = reg.Initialize(storeCfg, deps)
	if err != nil {
		return nil, err
	}

	if err := l.initTelemetry(); err != nil {
		return nil, errors.Join(err, l.Close(ctx))
	}

	l.initClock(ctx)

	return l, nil
}

// Never constructs the store, so the result is disabled and only useful for describing rules
func ConstructPolicies(ctx context.Context, cfg config.Config, secreter secret.Secreter, messages config.ErrorMessages) (*Limiter, error) {
	l, _, _, err := compileLimiter(ctx, cfg, secreter, messages)
	if err != nil {
		return nil, err
	}

	l.enabled = false

	return l, nil
}

func compileLimiter(ctx context.Context, cfg config.Config, secreter secret.Secreter, messages config.ErrorMessages) (*Limiter, ratelimit.RateLimiterRegistration, map[string]interface{}, error) {
	rawConfig := configload.ReplaceEnv(cfg.RateLimit)
	if rawConfig == nil {
		rawConfig = map[string]interface{}{"name": noneName}
	}

	var err error

	if secreter != nil {
		rawConfig, err = configload.ReplaceConfigValues(rawConfig, "secret", func(k string) (string, error) {
			v, _, lookupErr := secreter.Lookup(ctx, k)
			return v, lookupErr
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}

	name, _ := rawConfig["name"].(string)

	reg, ok := ratelimit.RegisteredRateLimiter(name)
	if !ok {
		nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
		return nil, nil, nil, fmt.Errorf(messages.EnumError, "ratelimit.name", nameCoerce, ratelimit.RegisteredRateLimiterNames())
	}

	commonRaw, storeRaw := splitConfig(rawConfig)

	var common CommonConfig
	if err := configload.DecodeEntityConfig(commonRaw, &common); err != nil {
		return nil, nil, nil, fmt.Errorf(messages.InvalidParam, "ratelimit", err.Error())
	}

	l := &Limiter{
		enabled:   name != noneName,
		storeName: name,
		now:       time.Now,
		audited:   newLRU(auditMemory),
	}

	if err := l.applyOptions(common, name, messages); err != nil {
		return nil, nil, nil, err
	}

	l.policies, err = compilePolicies(common.Policies, authenticationEnabled(cfg.Authentication), messages)
	if err != nil {
		return nil, nil, nil, err
	}

	if err := l.resolveLayers(cfg, messages); err != nil {
		return nil, nil, nil, err
	}

	return l, reg, storeRaw, nil
}

func (l *Limiter) applyOptions(common CommonConfig, name string, messages config.ErrorMessages) error {
	if name != noneName && len(common.Policies) == 0 {
		return fmt.Errorf(messages.ParamRequired, "ratelimit.policies")
	}

	l.headers = strings.ToLower(common.Headers)
	if l.headers == "" {
		l.headers = HeadersStandard
	}

	if !slices.Contains(allHeaders, l.headers) {
		return fmt.Errorf(messages.EnumError, "ratelimit.headers", common.Headers, allHeaders)
	}

	l.onError = strings.ToLower(common.OnError)
	if l.onError == "" {
		l.onError = OnErrorAllow
	}

	if !slices.Contains(allOnError, l.onError) {
		return fmt.Errorf(messages.EnumError, "ratelimit.onerror", common.OnError, allOnError)
	}

	l.timeout = defaultTimeout
	if common.Timeout > 0 {
		l.timeout = time.Duration(common.Timeout) * time.Millisecond // #nosec G115 -- operator-supplied timeout in milliseconds, far below int64 overflow range
	}

	l.ipv6Prefix = defaultIPv6Prefix
	if common.IPv6Prefix != nil {
		if *common.IPv6Prefix > maxIPv6Prefix {
			return fmt.Errorf(messages.RangeError, "ratelimit.ipv6prefix", 0, maxIPv6Prefix)
		}

		l.ipv6Prefix = int(*common.IPv6Prefix) // #nosec G115 -- bounded to 128 above
	}

	return nil
}

func (l *Limiter) resolveLayers(cfg config.Config, messages config.ErrorMessages) error {
	l.byID = make(map[string]*policy, len(l.policies))
	ids := make([]string, 0, len(l.policies))

	for _, p := range l.policies {
		l.byID[p.id] = p
		ids = append(ids, p.id)
	}

	if cfg.DefaultRateLimit != "" {
		l.defaultPolicy = l.byID[cfg.DefaultRateLimit]

		if l.defaultPolicy == nil {
			return fmt.Errorf(messages.EnumError, "defaultratelimit", cfg.DefaultRateLimit, ids)
		}
	} else if len(l.policies) > 0 {
		l.defaultPolicy = l.policies[0]
	}

	l.layerPolicies = make(map[string]*policy, len(cfg.Layers))
	l.layerIDs = make([]string, 0, len(cfg.Layers))

	for i, layer := range cfg.Layers {
		if layer.SkipRateLimit {
			if layer.RateLimit != "" {
				return fmt.Errorf(messages.ParamsMutuallyExclusive, fmt.Sprintf("layers[%d].ratelimit", i), fmt.Sprintf("layers[%d].skipratelimit", i))
			}

			continue
		}

		p := l.defaultPolicy

		if layer.RateLimit != "" {
			p = l.byID[layer.RateLimit]

			if p == nil {
				return fmt.Errorf(messages.EnumError, fmt.Sprintf("layers[%d].ratelimit", i), layer.RateLimit, ids)
			}
		}

		if p != nil {
			l.layerPolicies[layer.ID] = p
			l.layerIDs = append(l.layerIDs, layer.ID)
		}
	}

	return nil
}

func authenticationEnabled(rawAuth map[string]interface{}) bool {
	name, _ := configload.ReplaceEnv(rawAuth)["name"].(string)

	return name != "" && name != noneName
}
