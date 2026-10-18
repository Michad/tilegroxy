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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

const tenantKeySeparator = "~"

// Namespaces keys by tenant so per-tenant provider output can't poison other tenants. No tenant uses the plain key
type TenantCache struct {
	Cache cache.Cache
}

func NewTenantCache(inner cache.Cache) *TenantCache {
	return &TenantCache{Cache: inner}
}

type TenantConfig struct {
	Cache map[string]interface{}
}

func init() {
	cache.RegisterCache(TenantRegistration{})
}

type TenantRegistration struct {
}

func (s TenantRegistration) InitializeConfig() any {
	return TenantConfig{}
}

func (s TenantRegistration) Name() string {
	return "tenant"
}

func (s TenantRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(TenantConfig)

	inner, err := ConstructCache(config.Cache, deps)
	if err != nil {
		return nil, err
	}

	return NewTenantCache(inner), nil
}

// Tenant ID comes from a User's auth token, so it's sanitized like LayerName before joining the key
func namespace(ctx context.Context, t pkg.TileRequest) pkg.TileRequest {
	tenantID, ok := pkg.TenantIDFromContext(ctx)
	if !ok || tenantID == nil || *tenantID == "" {
		return t
	}

	t.LayerName = fmt.Sprintf("%s%s%s", safeLayerName(*tenantID), tenantKeySeparator, t.LayerName)
	return t
}

func (c *TenantCache) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	return c.Cache.Lookup(ctx, namespace(ctx, t))
}

func (c *TenantCache) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	return c.Cache.Save(ctx, namespace(ctx, t), img)
}

func (c *TenantCache) Remove(ctx context.Context, t pkg.TileRequest) (bool, error) {
	return c.Cache.Remove(ctx, namespace(ctx, t))
}

func (c *TenantCache) KeyedByIdentity() bool {
	return true
}

func (c *TenantCache) Children() []cache.Cache {
	return []cache.Cache{c.Cache}
}

func (c *TenantCache) Close(ctx context.Context) error {
	return lifecycle.CloseIfCloser(ctx, c.Cache)
}
