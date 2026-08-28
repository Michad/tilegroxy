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

package cache

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type Cache interface {
	Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error)
	Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error
}

// CacheDeps carries everything a cache is given at construction. New dependencies are added as fields so
// the Initialize signature stays stable
type CacheDeps struct {
	ErrorMessages config.ErrorMessages
}

type CacheRegistration interface {
	Name() string
	Initialize(config any, deps CacheDeps) (Cache, error)
	InitializeConfig() any
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]CacheRegistration)

// Registration is normally only done from init(), which Go serializes, but the mutex covers a
// consumer that registers concurrently instead.
func RegisterCache(reg CacheRegistration) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[reg.Name()] = reg
}

func RegisteredCache(name string) (CacheRegistration, bool) {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	o, ok := registrations[name]
	return o, ok
}

func RegisteredCacheNames() []string {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

// nonBlockingReadKey is the config key that turns on racing cache reads against tile generation.
// It's handled here rather than in each cache's own Config struct since it applies uniformly to
// every cache implementation, the same way "name" does.
const nonBlockingReadKey = "nonblockingread"

func ConstructCache(rawConfig map[string]interface{}, deps CacheDeps) (Cache, error) {
	name, ok := rawConfig["name"].(string)

	if ok {
		// An alias for the no-op cache, so fixtures can name an obviously-fake cache. It goes
		// through the same construction path operator config does, so `cache: {name: test}` in
		// production is a no-op cache rather than an error.
		if name == "test" || name == "Test" {
			name = "none"
		}

		reg, ok := RegisteredCache(name)
		if ok {
			nonBlockingRead, err := extractNonBlockingRead(rawConfig, deps.ErrorMessages)
			if err != nil {
				return nil, err
			}

			cfg := reg.InitializeConfig()
			err = config.DecodeEntityConfig(withoutNonBlockingRead(rawConfig), &cfg)
			if err != nil {
				return nil, err
			}
			a, err := reg.Initialize(cfg, deps)
			return CacheWrapper{Name: name, Cache: a, NonBlockingRead: nonBlockingRead}, err
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "cache.name", nameCoerce, RegisteredCacheNames())
}

// extractNonBlockingRead reads the shared nonblockingread flag out of a cache's raw config. Defaults
// to false (today's sequential cache-then-generate behavior) when absent.
func extractNonBlockingRead(rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (bool, error) {
	raw, ok := rawConfig[nonBlockingReadKey]
	if !ok {
		return false, nil
	}

	b, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf(errorMessages.InvalidParam, "cache.nonblockingread", fmt.Sprintf("%v", raw))
	}

	return b, nil
}

// withoutNonBlockingRead strips the shared key before decoding into a cache-specific Config struct,
// same reasoning as why DecodeEntityConfig strips "name": no cache declares this field itself.
func withoutNonBlockingRead(rawConfig map[string]interface{}) map[string]interface{} {
	stripped := make(map[string]interface{}, len(rawConfig))
	for k, v := range rawConfig {
		if strings.EqualFold(k, nonBlockingReadKey) {
			continue
		}
		stripped[k] = v
	}
	return stripped
}
