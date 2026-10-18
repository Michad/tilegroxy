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
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

type Cache interface {
	Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error)
	Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error
	// Reports whether an entry was there. A miss isn't an error
	Remove(ctx context.Context, t pkg.TileRequest) (bool, error)
}

// New dependencies are added as fields so the Initialize signature stays stable
type CacheDeps struct {
	ErrorMessages config.ErrorMessages
	Datastores    datastore.DatastoreRegistry
}

type CacheRegistration interface {
	Name() string
	Initialize(config any, deps CacheDeps) (Cache, error)
	InitializeConfig() any
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]CacheRegistration)

// init() is serialized by Go, but the mutex covers consumers that register concurrently
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

// Gives entities access to the caches configured by ID
type CacheRegistry interface {
	Get(id string) (Cache, bool)
	IDs() []string
}
