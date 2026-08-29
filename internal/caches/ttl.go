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

package caches

import (
	"context"
	"fmt"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

// TTLCache wraps any Cache to enforce a uniform expiration, emulating it for backends with no
// native notion of TTL. An expired entry is reported as a cache miss rather than deleted: the
// normal request path then regenerates and overwrites it, which is all a per-layer TTL needs.
type TTLCache struct {
	Cache cache.Cache
	TTL   time.Duration
	// clock returns the current time. Overridden in tests, defaults to time.Now.
	clock func() time.Time
}

func NewTTLCache(inner cache.Cache, ttl time.Duration) *TTLCache {
	return &TTLCache{Cache: inner, TTL: ttl}
}

type TTLConfig struct {
	TTL   uint32 // Maximum time to live of a tile in seconds. Required
	Cache map[string]interface{}
}

func init() {
	cache.RegisterCache(TTLRegistration{})
}

type TTLRegistration struct {
}

func (s TTLRegistration) InitializeConfig() any {
	return TTLConfig{}
}

func (s TTLRegistration) Name() string {
	return "ttl"
}

func (s TTLRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(TTLConfig)

	if config.TTL < 1 {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "cache.ttl.ttl", config.TTL)
	}

	inner, err := cache.ConstructCache(config.Cache, deps)
	if err != nil {
		return nil, err
	}

	return NewTTLCache(inner, time.Duration(config.TTL)*time.Second), nil
}

func (c *TTLCache) now() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

func (c *TTLCache) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	img, err := c.Cache.Lookup(ctx, t)
	if err != nil || img == nil {
		return img, err
	}

	// CreatedAt is zero for entries written before TTL support existed, or by a path that
	// bypasses TTLCache. Treat those as always fresh rather than always expired.
	if img.CreatedAt != 0 && c.now().Sub(time.Unix(img.CreatedAt, 0)) > c.TTL {
		return nil, nil
	}

	return img, nil
}

func (c *TTLCache) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	img.CreatedAt = c.now().Unix()
	return c.Cache.Save(ctx, t, img)
}

func (c *TTLCache) Close(ctx context.Context) error {
	return lifecycle.CloseIfCloser(ctx, c.Cache)
}
