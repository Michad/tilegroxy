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
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"

	"github.com/maypok86/otter"
)

const defaultMaxSize = 100
const minMaxSize = 10
const defaultTTL = 3600

type MemoryConfig struct {
	MaxSize uint16 // Maximum number of tiles to hold in the cache. Defaults to 100
	TTL     uint32 // Maximum time to live of a tile in seconds. Defaults to 3600 (1 hour)
}

type Memory struct {
	MemoryConfig
	Cache otter.Cache[string, pkg.Image]
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

func (s MemoryRegistration) Initialize(configAny any, _ config.ErrorMessages) (cache.Cache, error) {
	config := configAny.(MemoryConfig)

	if config.MaxSize < 1 {
		config.MaxSize = defaultMaxSize
	}
	if config.MaxSize < minMaxSize {
		config.MaxSize = minMaxSize
	}

	if config.TTL < 1 {
		config.TTL = defaultTTL
	}

	cache, err := otter.MustBuilder[string, pkg.Image](int(config.MaxSize)).
		WithTTL(time.Duration(config.TTL) * time.Second).
		Build()
	if err != nil {
		return nil, err
	}

	return &Memory{config, cache}, nil
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
