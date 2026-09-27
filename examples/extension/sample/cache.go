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

package sample

import (
	"context"
	"fmt"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
)

type CacheConfig struct {
	Datastore string
}

// Cache stores tiles in a sample datastore
type Cache struct {
	store *sync.Map
}

func init() {
	cache.RegisterCache(CacheRegistration{})
}

type CacheRegistration struct{}

func (CacheRegistration) InitializeConfig() any {
	return CacheConfig{}
}

func (CacheRegistration) Name() string {
	return "sample"
}

func (CacheRegistration) Initialize(cfgAny any, deps cache.CacheDeps) (cache.Cache, error) {
	cfg := cfgAny.(CacheConfig)

	ds, ok := deps.Datastores.Get(cfg.Datastore)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.sample.datastore", cfg.Datastore)
	}

	store, ok := ds.Native().(*sync.Map)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.sample.datastore", cfg.Datastore)
	}

	return &Cache{store}, nil
}

func (c *Cache) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	value, ok := c.store.Load(t.String())
	if !ok {
		return nil, nil
	}

	return pkg.DecodeImage(value.([]byte))
}

func (c *Cache) Save(_ context.Context, t pkg.TileRequest, img *pkg.Image) error {
	encoded, err := img.Encode()
	if err != nil {
		return err
	}

	c.store.Store(t.String(), encoded)

	return nil
}

func (c *Cache) Remove(_ context.Context, t pkg.TileRequest) (bool, error) {
	_, existed := c.store.LoadAndDelete(t.String())

	return existed, nil
}
