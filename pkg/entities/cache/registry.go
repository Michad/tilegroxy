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

package cache

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// CacheRegistry holds every cache declared at the top level of the configuration, keyed by the id
// layers reference. Each entry owns its own tree of nested caches, so no two entries share an
// instance and closing them is unambiguous.
type CacheRegistry struct {
	caches map[string]Cache
	order  []string
	// The ids of top-level entries, in declaration order. Nested caches are reachable through
	// caches but are closed by the parent that built them, so they're excluded here.
	owned     []string
	defaultID string
}

// NewSingleCacheRegistry wraps one already-constructed cache as the sole, default entry.
func NewSingleCacheRegistry(c Cache) *CacheRegistry {
	return &CacheRegistry{
		caches:    map[string]Cache{config.DefaultCacheID: c},
		order:     []string{config.DefaultCacheID},
		owned:     []string{config.DefaultCacheID},
		defaultID: config.DefaultCacheID,
	}
}

func (reg *CacheRegistry) Get(id string) (Cache, bool) {
	if reg == nil {
		return nil, false
	}

	res, ok := reg.caches[id]

	return res, ok
}

// Default returns the cache used by layers that don't name one.
func (reg *CacheRegistry) Default() Cache {
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

// IDs lists the configured ids in declaration order, for error messages naming the valid choices.
func (reg *CacheRegistry) IDs() []string {
	if reg == nil {
		return nil
	}

	return reg.order
}

// Close releases every cache that holds resources. Called on shutdown and after a hot reload swaps
// in a new generation of entities.
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

// registerNested makes caches defined inside another cache referenceable by their own id, so a
// layer can point at one tier of a multi cache. The built graph carries no ids, so the raw config
// is walked alongside it. Nested entries are not added to owned: the parent closes them.
func (reg *CacheRegistry) registerNested(rawConfig map[string]interface{}, built Cache, errorMessages config.ErrorMessages) error {
	for i, childConfig := range nestedCacheConfigs(rawConfig) {
		childBuilt, ok := nestedCache(built, i)
		if !ok {
			continue
		}

		id, explicit := childConfig["id"].(string)
		if !explicit || id == "" {
			// Falling back to name is what makes the nested cache in the docs' example addressable
			// without an explicit id. Two tiers of the same kind is legal config though, so a
			// collision here only costs the ability to reference them; an explicit id is the fix.
			id, _ = childConfig["name"].(string)
			explicit = false
		}

		if id != "" {
			if _, taken := reg.caches[id]; taken {
				if explicit {
					return fmt.Errorf(errorMessages.MustBeUnique, "cache.id", id)
				}
			} else {
				reg.caches[id] = childBuilt
				reg.order = append(reg.order, id)
			}
		}

		if err := reg.registerNested(childConfig, childBuilt, errorMessages); err != nil {
			return err
		}
	}

	return nil
}

// nestedCache pulls the i-th child out of a constructed cache. Caches live in internal packages
// that pkg can't import, so this reads the exported Tiers/Cache fields reflectively rather than
// type switching. Every nesting cache holds its children in one of those two shapes.
func nestedCache(built Cache, i int) (Cache, bool) {
	value := reflect.ValueOf(unwrap(built))
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}

		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return nil, false
	}

	if tiers := value.FieldByName("Tiers"); tiers.IsValid() && tiers.Kind() == reflect.Slice {
		if i >= tiers.Len() {
			return nil, false
		}

		child, ok := tiers.Index(i).Interface().(Cache)

		return child, ok
	}

	if inner := value.FieldByName("Cache"); inner.IsValid() && i == 0 {
		child, ok := inner.Interface().(Cache)

		return child, ok
	}

	return nil, false
}

// unwrap steps past the telemetry wrapper to the cache that actually holds the children.
func unwrap(c Cache) Cache {
	if wrapper, ok := c.(CacheWrapper); ok {
		return wrapper.Cache
	}

	return c
}

// nestedCacheConfigs returns the child cache configs of a cache config, in the order the cache
// itself constructs them. `tiers` covers multi, `cache` covers the single-child wrappers.
func nestedCacheConfigs(rawConfig map[string]interface{}) []map[string]interface{} {
	for key, value := range rawConfig {
		switch strings.ToLower(key) {
		case "tiers":
			return toConfigList(value)
		case "cache":
			if child, ok := value.(map[string]interface{}); ok {
				return []map[string]interface{}{child}
			}
		}
	}

	return nil
}

func toConfigList(value any) []map[string]interface{} {
	switch typed := value.(type) {
	case []map[string]interface{}:
		return typed
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(typed))

		for _, entry := range typed {
			child, ok := entry.(map[string]interface{})
			if !ok {
				return nil
			}

			result = append(result, child)
		}

		return result
	}

	return nil
}

// NewUnknownCacheError reports a reference to a cache id that isn't configured, listing what is.
func NewUnknownCacheError(errorMessages config.ErrorMessages, param, id string, valid []string) error {
	return fmt.Errorf(errorMessages.EnumError, param, id, valid)
}

// ConstructCacheRegistry builds every configured cache. rawConfig is either a single cache or an
// array of them; defaultID names the entry layers fall back to and defaults to the first.
func ConstructCacheRegistry(rawConfig interface{}, defaultID string, secreter secret.Secreter, deps CacheDeps) (*CacheRegistry, error) {
	entries, err := config.NormalizeCaches(rawConfig, deps.ErrorMessages)
	if err != nil {
		return nil, err
	}

	reg := CacheRegistry{
		caches: make(map[string]Cache, len(entries)),
		order:  make([]string, 0, len(entries)),
	}

	for _, entry := range entries {
		if _, taken := reg.caches[entry.ID]; taken {
			closeErr := reg.Close(context.Background())
			return nil, errors.Join(fmt.Errorf(deps.ErrorMessages.MustBeUnique, "cache.id", entry.ID), closeErr)
		}

		cfg := pkg.ReplaceEnv(entry.Config)

		if secreter != nil {
			cfg, err = pkg.ReplaceConfigValues(cfg, "secret", secreter.Lookup)
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

		if err := reg.registerNested(cfg, built, deps.ErrorMessages); err != nil {
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
