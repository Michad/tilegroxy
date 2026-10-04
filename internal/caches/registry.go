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

package caches

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// Top-level caches keyed by the id layers reference. Each entry owns its nested tree so closing is unambiguous
type CacheRegistry struct {
	caches map[string]cache.Cache
	order  []string
	// Top-level ids in declaration order. Nested caches are closed by their parent so they're excluded
	owned     []string
	defaultID string
}

// For tests
func NewSingleCacheRegistry(c cache.Cache) *CacheRegistry {
	ID := "test-single"
	return &CacheRegistry{
		caches:    map[string]cache.Cache{ID: c},
		order:     []string{ID},
		owned:     []string{ID},
		defaultID: ID,
	}
}

func (reg *CacheRegistry) Get(id string) (cache.Cache, bool) {
	if reg == nil {
		return nil, false
	}

	res, ok := reg.caches[id]

	return res, ok
}

// Used by layers that don't name a cache
func (reg *CacheRegistry) Default() cache.Cache {
	if reg == nil {
		return nil
	}

	return reg.caches[reg.defaultID]
}

func (reg *CacheRegistry) DefaultID() string {
	if reg == nil {
		return ""
	}

	return reg.defaultID
}

// Declaration order, for error messages naming the valid choices
func (reg *CacheRegistry) IDs() []string {
	if reg == nil {
		return nil
	}

	return reg.order
}

// Called on shutdown and after a hot reload swaps in a new generation of entities
func (reg *CacheRegistry) Close(ctx context.Context) error {
	if reg == nil {
		return nil
	}

	errs := make([]error, 0, len(reg.owned))

	for _, id := range reg.owned {
		errs = append(errs, lifecycle.CloseIfCloser(ctx, reg.caches[id]))
	}

	return errors.Join(errs...)
}

// Lets a layer reference one tier of a multi cache by id. Nested entries are closed by their parent
func (reg *CacheRegistry) registerNested(built cache.Cache, errorMessages config.ErrorMessages) error {
	for _, child := range children(built) {
		if wrapper, ok := child.(CacheWrapper); ok && wrapper.ref != "" {
			if _, taken := reg.caches[wrapper.ref]; taken {
				// Same-kind tiers are legal, so a name collision only leaves them unreferenceable until given explicit ids
				if wrapper.refExplicit {
					return fmt.Errorf(errorMessages.MustBeUnique, "cache.id", wrapper.ref)
				}
			} else {
				reg.caches[wrapper.ref] = child
				reg.order = append(reg.order, wrapper.ref)
			}
		}

		if err := reg.registerNested(child, errorMessages); err != nil {
			return err
		}
	}

	return nil
}

// The shortest TTL wins when several Expiring caches are nested
func ExtractTTLFromCache(built cache.Cache) (time.Duration, bool) {
	var shortest time.Duration
	found := false

	walk(built, func(node cache.Cache) {
		e, ok := as[cache.Expiring](node)
		if !ok {
			return
		}

		if ttl := e.TTL(); ttl > 0 && (!found || ttl < shortest) {
			shortest = ttl
			found = true
		}
	})

	return shortest, found
}

// Lists the configured ids
func NewUnknownCacheError(errorMessages config.ErrorMessages, param, id string, valid []string) error {
	return fmt.Errorf(errorMessages.EnumError, param, id, valid)
}

// rawConfig is a single cache or an array. defaultID names the fallback entry and defaults to the first
func ConstructCacheRegistry(ctx context.Context, rawConfig interface{}, defaultID string, secreter secret.Secreter, deps cache.CacheDeps) (*CacheRegistry, error) {
	entries, err := configload.NormalizeCaches(rawConfig, deps.ErrorMessages)
	if err != nil {
		return nil, err
	}

	reg := CacheRegistry{
		caches: make(map[string]cache.Cache, len(entries)),
		order:  make([]string, 0, len(entries)),
	}

	for _, entry := range entries {
		if _, taken := reg.caches[entry.ID]; taken {
			closeErr := reg.Close(context.Background())
			return nil, errors.Join(fmt.Errorf(deps.ErrorMessages.MustBeUnique, "cache.id", entry.ID), closeErr)
		}

		cfg := configload.ReplaceEnv(entry.Config)

		if secreter != nil {
			cfg, err = configload.ReplaceConfigValues(cfg, "secret", func(k string) (string, error) {
				v, _, lookupErr := secreter.Lookup(ctx, k)
				return v, lookupErr
			})
			if err != nil {
				closeErr := reg.Close(context.Background())
				return nil, errors.Join(err, closeErr)
			}
		}

		built, err := ConstructCache(cfg, deps)
		if err != nil {
			closeErr := reg.Close(context.Background())
			return nil, errors.Join(err, closeErr)
		}

		reg.caches[entry.ID] = built
		reg.order = append(reg.order, entry.ID)
		reg.owned = append(reg.owned, entry.ID)

		if err := reg.registerNested(built, deps.ErrorMessages); err != nil {
			closeErr := reg.Close(context.Background())
			return nil, errors.Join(err, closeErr)
		}
	}

	if defaultID == "" {
		reg.defaultID = reg.order[0]
	} else {
		if _, ok := reg.caches[defaultID]; !ok {
			closeErr := reg.Close(context.Background())
			return nil, errors.Join(NewUnknownCacheError(deps.ErrorMessages, "defaultcache", defaultID, reg.order), closeErr)
		}

		reg.defaultID = defaultID
	}

	return &reg, nil
}

// The id selects the cache like name selects its type, and would otherwise fail the unknown-key check
func withoutCacheID(rawConfig map[string]interface{}) map[string]interface{} {
	found := false

	for k := range rawConfig {
		if strings.EqualFold(k, "id") {
			found = true
			break
		}
	}

	if !found {
		return rawConfig
	}

	stripped := make(map[string]interface{}, len(rawConfig))

	for k, v := range rawConfig {
		if !strings.EqualFold(k, "id") {
			stripped[k] = v
		}
	}

	return stripped
}

func ConstructCache(rawConfig map[string]interface{}, deps cache.CacheDeps) (cache.Cache, error) {
	name, ok := rawConfig["name"].(string)

	if ok {
		ref, refExplicit := rawConfig["id"].(string)
		if !refExplicit || ref == "" {
			ref, refExplicit = name, false
		}

		// Lets fixtures name an obviously-fake cache. Goes through the operator config path, so it's valid in production too
		if name == "test" || name == "Test" {
			name = "none"
		}

		reg, ok := cache.RegisteredCache(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := configload.DecodeEntityConfig(withoutCacheID(rawConfig), &cfg)
			if err != nil {
				return nil, err
			}
			a, err := reg.Initialize(cfg, deps)
			return CacheWrapper{Name: name, Cache: a, ref: ref, refExplicit: refExplicit}, err
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "cache.name", nameCoerce, cache.RegisteredCacheNames())
}
