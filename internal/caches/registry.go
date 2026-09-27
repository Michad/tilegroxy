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
	"reflect"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// CacheRegistry holds every cache declared at the top level of the configuration, keyed by the id
// layers reference. Each entry owns its own tree of nested caches, so no two entries share an
// instance and closing them is unambiguous.
type CacheRegistry struct {
	caches map[string]cache.Cache
	order  []string
	// The ids of top-level entries, in declaration order. Nested caches are reachable through
	// caches but are closed by the parent that built them, so they're excluded here.
	owned     []string
	defaultID string
}

// A helper for tests that creates a singleton registry with a single item
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

// Default returns the cache used by layers that don't name one.
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
func (reg *CacheRegistry) registerNested(rawConfig map[string]interface{}, built cache.Cache, errorMessages config.ErrorMessages) error {
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

func ContainsCache(built cache.Cache, name string) bool {
	if wrapper, ok := built.(CacheWrapper); ok {
		if wrapper.Name == name {
			return true
		}
	}

	for i := 0; ; i++ {
		child, ok := nestedCache(built, i)
		if !ok {
			return false
		}

		if ContainsCache(child, name) {
			return true
		}
	}
}

// Finds the lifetime of a ttl cache anywhere in a chain. The shortest wins when several are nested
func ExtractTTLFromCache(built cache.Cache) (time.Duration, bool) {
	var shortest time.Duration
	found := false

	if wrapper, ok := built.(CacheWrapper); ok && wrapper.Name == "ttl" {
		if ttl, ok := findTTLViaReflect(wrapper.Cache); ok {
			shortest = ttl
			found = true
		}
	}

	for i := 0; ; i++ {
		child, ok := nestedCache(built, i)
		if !ok {
			return shortest, found
		}

		if ttl, ok := ExtractTTLFromCache(child); ok && (!found || ttl < shortest) {
			shortest = ttl
			found = true
		}
	}
}

// Reads an exported TTL field reflectively, the same way nested children are found.
func findTTLViaReflect(built cache.Cache) (time.Duration, bool) {
	value := reflect.ValueOf(unwrap(built))
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}

		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return 0, false
	}

	field := value.FieldByName("TTL")
	if !field.IsValid() || !field.CanInterface() {
		return 0, false
	}

	ttl, ok := field.Interface().(time.Duration)

	return ttl, ok && ttl > 0
}

// nestedCache pulls the i-th child out of a constructed cache by reading the exported Tiers/Cache
// fields reflectively. Every nesting cache holds its children in one of those two shapes.
func nestedCache(built cache.Cache, i int) (cache.Cache, bool) {
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

		child, ok := tiers.Index(i).Interface().(cache.Cache)

		return child, ok
	}

	if inner := value.FieldByName("Cache"); inner.IsValid() && i == 0 {
		child, ok := inner.Interface().(cache.Cache)

		return child, ok
	}

	return nil, false
}

// unwrap steps past the telemetry wrapper to the cache that actually holds the children.
func unwrap(c cache.Cache) cache.Cache {
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

// withoutCacheID drops the id naming a top-level cache entry, which selects the cache the way name
// selects its type. No cache config declares one, so leaving it in would fail the unknown-key check.
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
		// An alias for the no-op cache, so fixtures can name an obviously-fake cache. It goes
		// through the same construction path operator config does, so `cache: {name: test}` in
		// production is a no-op cache rather than an error.
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
			return CacheWrapper{Name: name, Cache: a}, err
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "cache.name", nameCoerce, cache.RegisteredCacheNames())
}
