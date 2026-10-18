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

package health

import (
	"context"
	"sync"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type HealthCheck interface {
	Check(ctx context.Context) error
	GetDelay() uint
}

type HealthCheckConfig interface {
	GetDelay() uint
}

// New dependencies are added as fields so the Initialize signature stays stable
type HealthCheckDeps struct {
	LayerGroup layer.LayerGroup
	Caches     cache.CacheRegistry
	// A check may need settings outside its own block
	AllConfig *config.Config
}

type HealthCheckRegistration interface {
	Name() string
	Initialize(checkConfig HealthCheckConfig, deps HealthCheckDeps) (HealthCheck, error)
	InitializeConfig() HealthCheckConfig
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]HealthCheckRegistration)

func RegisterHealthCheck(reg HealthCheckRegistration) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[reg.Name()] = reg
}

func RegisteredHealthCheck(name string) (HealthCheckRegistration, bool) {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	o, ok := registrations[name]
	return o, ok
}

func RegisteredHealthCheckNames() []string {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}
