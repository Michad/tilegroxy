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
	"time"

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

// A helper for tests that creates a singleton registry with a single item
func NewSingleCacheRegistry(c Cache) *CacheRegistry {
	ID := "test-single"
	return &CacheRegistry{
		caches:    map[string]Cache{ID: c},
		order:     []string{ID},
		owned:     []string{ID},
		defaultID: ID,
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

// Lets a layer reference one tier of a multi cache by id. Nested entries aren't owned: the parent closes them.
func (reg *CacheRegistry) registerNested(built Cache, errorMessages config.ErrorMessages) error {
	for _, child := range children(built) {
		if wrapper, ok := child.(CacheWrapper); ok && wrapper.ref != "" {
			if _, taken := reg.caches[wrapper.ref]; taken {
				// Same-kind tiers are legal, so a name collision just leaves them unreferenceable; an explicit id fixes that.
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

// ContainsCache reports whether a cache registered under name is anywhere in the tree. Prefer the capability interfaces.
func ContainsCache(built Cache, name string) bool {
	found := false

	walk(built, func(node Cache) {
		if wrapper, ok := node.(CacheWrapper); ok && wrapper.Name == name {
			found = true
		}
	})

	return found
}

// ExtractTTLFromCache finds the lifetime of any Expiring cache in the tree. The shortest wins when several are nested.
func ExtractTTLFromCache(built Cache) (time.Duration, bool) {
	var shortest time.Duration
	found := false

	walk(built, func(node Cache) {
		e, ok := as[Expiring](node)
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

// NewUnknownCacheError reports a reference to a cache id that isn't configured, listing what is.
func NewUnknownCacheError(errorMessages config.ErrorMessages, param, id string, valid []string) error {
	return fmt.Errorf(errorMessages.EnumError, param, id, valid)
}

// ConstructCacheRegistry builds every configured cache. rawConfig is either a single cache or an
// array of them; defaultID names the entry layers fall back to and defaults to the first.
func ConstructCacheRegistry(ctx context.Context, rawConfig interface{}, defaultID string, secreter secret.Secreter, deps CacheDeps) (*CacheRegistry, error) {
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
			cfg, err = pkg.ReplaceConfigValues(cfg, "secret", func(k string) (string, error) {
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
