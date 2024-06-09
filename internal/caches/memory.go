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
	"time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"

	"github.com/maypok86/otter"
)

type MemoryConfig struct {
	MaxSize uint16 //Maximum number of tiles to hold in the cache. Defaults to 100
	Ttl     uint32 //Maximum time to live of a tile in seconds. Defaultss to 3600 (1 hour)
}

type Memory struct {
	MemoryConfig
	Cache otter.Cache[string, []byte]
}

func ConstructMemory(config MemoryConfig, ErrorMessages *config.ErrorMessages) (*Memory, error) {
	if config.MaxSize < 1 {
		config.MaxSize = 100
	}

	if config.Ttl < 1 {
		config.Ttl = 3600
	}

	cache, err := otter.MustBuilder[string, internal.Image](int(config.MaxSize)).
		WithTTL(time.Duration(config.Ttl * uint32(time.Second))).
		Build()
	if err != nil {
		return nil, err
	}

	return &Memory{config, cache}, nil
}

func (c Memory) Lookup(t internal.TileRequest) (*internal.Image, error) {
	img, ok := c.Cache.Get(t.String())

	if ok {
		return &img, nil
	}

	return nil, nil
}

func (c Memory) Save(t internal.TileRequest, img *internal.Image) error {
	c.Cache.Set(t.String(), *img)
	return nil
}
