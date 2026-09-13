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

package caches

import (
	"context"
	"fmt"

	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"

	"github.com/maypok86/otter"
)

type MemoryConfig struct {
	MaxSize   uint16 // Maximum number of tiles to hold in the cache. Defaults to 100
	TTL       uint32 // Maximum time to live of a tile in seconds. Defaults to 3600 (1 hour)
	Datastore string // ID of a memory datastore to share instead of holding a private store. Mutually exclusive with the parameters above
}

type Memory struct {
	MemoryConfig
	Cache otter.Cache[string, pkg.Image]
	// Held only so it can be shut down. Nil when the store comes from a shared datastore, since the
	// datastore registry owns closing it in that case.
	owned *datastores.MemoryWrapper
}

func init() {
	cache.RegisterCache(MemoryRegistration{})
}

type MemoryRegistration struct {
}

func (s MemoryRegistration) InitializeConfig() any {
	return MemoryConfig{}
}

func (s MemoryRegistration) Name() string {
	return "memory"
}

func (s MemoryRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(MemoryConfig)

	if config.Datastore != "" {
		return initializeMemoryFromDatastore(config, deps)
	}

	// A private store, so two memory caches don't unexpectedly share tiles. Operators who do want
	// them shared point both at one datastore.
	owned, err := datastores.NewMemoryWrapper(datastores.MemoryWrapperConfig{MaxSize: config.MaxSize, TTL: config.TTL})
	if err != nil {
		return nil, err
	}

	store, _ := owned.Native().(otter.Cache[string, pkg.Image])

	return &Memory{config, store, owned}, nil
}

func initializeMemoryFromDatastore(config MemoryConfig, deps cache.CacheDeps) (cache.Cache, error) {
	if config.MaxSize != 0 || config.TTL != 0 {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "cache.memory.datastore", "cache.memory.maxsize and cache.memory.ttl")
	}

	ds, ok := deps.Datastores.Get(config.Datastore)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.memory.datastore", config.Datastore)
	}

	store, ok := ds.Native().(otter.Cache[string, pkg.Image])
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.memory.datastore", config.Datastore)
	}

	return &Memory{config, store, nil}, nil
}

// Close releases a private store. A shared one belongs to the datastore registry.
func (c Memory) Close(ctx context.Context) error {
	if c.owned == nil {
		return nil
	}

	return c.owned.Close(ctx)
}

func (c Memory) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	img, ok := c.Cache.Get(t.String())

	if ok {
		return &img, nil
	}

	return nil, nil
}

func (c Memory) Save(_ context.Context, t pkg.TileRequest, img *pkg.Image) error {
	c.Cache.Set(t.String(), *img)
	return nil
}

func (c Memory) Remove(_ context.Context, t pkg.TileRequest) (bool, error) {
	key := t.String()

	// otter's Delete reports nothing, so presence has to be checked separately.
	present := c.Cache.Has(key)
	c.Cache.Delete(key)

	return present, nil
}
