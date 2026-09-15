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

package datastores

import (
	"context"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"

	"github.com/maypok86/otter"
)

const (
	MemoryDefaultMaxSize = 100
	MemoryMinMaxSize     = 10
	MemoryDefaultTTL     = 3600
)

type MemoryWrapperConfig struct {
	ID      string
	MaxSize uint16 // Maximum number of tiles to hold. Defaults to 100
	TTL     uint32 // Maximum time to live of a tile in seconds. Defaults to 3600 (1 hour)
}

type MemoryWrapper struct {
	MemoryWrapperConfig
	cache otter.Cache[string, pkg.Image]
}

func init() {
	datastore.RegisterDatastoreWrapper(MemoryWrapperRegistration{})
}

type MemoryWrapperRegistration struct {
}

func (s MemoryWrapperRegistration) InitializeConfig() any {
	return MemoryWrapperConfig{}
}

func (s MemoryWrapperRegistration) Name() string {
	return "memory"
}

func (s MemoryWrapperRegistration) Initialize(cfgAny any, _ datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(MemoryWrapperConfig)

	return NewMemoryWrapper(cfg)
}

// NewMemoryWrapper builds an in-process tile store. Also used by the memory cache to construct a
// private, unregistered store when no datastore is named.
func NewMemoryWrapper(cfg MemoryWrapperConfig) (*MemoryWrapper, error) {
	if cfg.MaxSize < 1 {
		cfg.MaxSize = MemoryDefaultMaxSize
	}
	if cfg.MaxSize < MemoryMinMaxSize {
		cfg.MaxSize = MemoryMinMaxSize
	}

	if cfg.TTL < 1 {
		cfg.TTL = MemoryDefaultTTL
	}

	built, err := otter.MustBuilder[string, pkg.Image](int(cfg.MaxSize)).
		WithTTL(time.Duration(cfg.TTL) * time.Second).
		Build()
	if err != nil {
		return nil, err
	}

	return &MemoryWrapper{cfg, built}, nil
}

func (w MemoryWrapper) GetID() string {
	return w.ID
}

func (w MemoryWrapper) Native() any {
	return w.cache
}

// Close stops the otter maintenance goroutines backing the store.
func (w MemoryWrapper) Close(_ context.Context) error {
	w.cache.Close()
	return nil
}
