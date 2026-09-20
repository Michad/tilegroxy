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

package secret

import (
	"context"
	"fmt"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

// Secreter resolves secret references in a configuration. An implementation holding resources may also
// implement lifecycle.Closer, which is called when the configuration generation is released
type Secreter interface {
	// Lookup resolves key into its value and an opaque version identifier. An implementation without
	// change detection returns an empty version
	Lookup(ctx context.Context, key string) (string, string, error)

	// Check returns the current version of each key, positionally matching keys. An empty version
	// means the key cannot be watched for changes
	Check(ctx context.Context, keys []string) ([]string, error)
}

// SecreterDeps carries everything a secret module is given at construction. New dependencies are added as
// fields so the Initialize signature stays stable
type SecreterDeps struct {
	ErrorMessages config.ErrorMessages
	// ReloadFunc rebuilds the entity generation when a watched secret rotates. Nil disables watching
	ReloadFunc func()
}

type SecreterRegistration interface {
	Name() string
	Initialize(config any, deps SecreterDeps) (Secreter, error)
	InitializeConfig() any
	// CheckBatchSize caps how many keys Check accepts at once. 0 means the backend cannot be watched
	CheckBatchSize() int
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]SecreterRegistration)

func RegisterSecreter(reg SecreterRegistration) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[reg.Name()] = reg
}

func RegisteredSecreter(name string) (SecreterRegistration, bool) {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	o, ok := registrations[name]
	return o, ok
}

func RegisteredSecreterNames() []string {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

func ConstructSecreter(rawConfig map[string]interface{}, deps SecreterDeps) (Secreter, error) {
	rawConfig = pkg.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := RegisteredSecreter(name)
		if ok {
			watchCfg, stripped, err := parseWatchConfig(rawConfig, reg.CheckBatchSize(), deps.ErrorMessages)
			if err != nil {
				return nil, err
			}

			cfg := reg.InitializeConfig()
			if err := config.DecodeEntityConfig(stripped, &cfg); err != nil {
				return nil, err
			}

			built, err := reg.Initialize(cfg, deps)
			if err != nil {
				return nil, err
			}

			if !watchCfg.Watch || deps.ReloadFunc == nil {
				return built, nil
			}

			w := newWatchingSecreter(built, watchCfg, reg.CheckBatchSize(), deps.ReloadFunc)
			w.start(watchCfg)

			return w, nil
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "secret.name", nameCoerce, RegisteredSecreterNames())
}
