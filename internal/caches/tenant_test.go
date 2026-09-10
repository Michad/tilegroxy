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
	"net/http"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/require"
)

func contextWithTenant(t *testing.T, tenantID string) context.Context {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	ctx := pkg.NewRequestContext(req)

	tid, ok := pkg.TenantIDFromContext(ctx)
	require.True(t, ok)
	*tid = tenantID

	return ctx
}

func Test_TenantCache_NamespacesKeysByTenant(t *testing.T) {
	inner := newMemCache()
	c := NewTenantCache(inner)
	req := testTileRequest()

	ctxA := contextWithTenant(t, "tenant-a")
	ctxB := contextWithTenant(t, "tenant-b")

	require.NoError(t, c.Save(ctxA, req, &pkg.Image{Content: []byte("a-data"), ContentType: "image/png"}))
	require.NoError(t, c.Save(ctxB, req, &pkg.Image{Content: []byte("b-data"), ContentType: "image/png"}))

	imgA, err := c.Lookup(ctxA, req)
	require.NoError(t, err)
	require.NotNil(t, imgA)
	require.Equal(t, []byte("a-data"), imgA.Content)

	imgB, err := c.Lookup(ctxB, req)
	require.NoError(t, err)
	require.NotNil(t, imgB)
	require.Equal(t, []byte("b-data"), imgB.Content)

	// The two tenants must not share the same underlying key
	require.Len(t, inner.entries, 2)
}

func Test_TenantCache_NoTenantFallsBackToUnnamespacedKey(t *testing.T) {
	inner := newMemCache()
	c := NewTenantCache(inner)
	req := testTileRequest()

	ctx := contextWithTenant(t, "")

	require.NoError(t, c.Save(ctx, req, &pkg.Image{Content: []byte("data"), ContentType: "image/png"}))

	img, err := inner.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, []byte("data"), img.Content)
}

func Test_TenantCache_MissingTenantOnContextFallsBackToUnnamespacedKey(t *testing.T) {
	inner := newMemCache()
	c := NewTenantCache(inner)
	req := testTileRequest()

	// A plain background context has no tenant ID value at all, unlike a request context which
	// always seeds one (even if empty).
	ctx := context.Background()

	require.NoError(t, c.Save(ctx, req, &pkg.Image{Content: []byte("data"), ContentType: "image/png"}))

	img, err := inner.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, []byte("data"), img.Content)
}

func Test_TenantCache_SanitizesTenantIDForCacheKey(t *testing.T) {
	inner := newMemCache()
	c := NewTenantCache(inner)
	req := testTileRequest()

	ctx := contextWithTenant(t, "../../etc")

	require.NoError(t, c.Save(ctx, req, &pkg.Image{Content: []byte("data"), ContentType: "image/png"}))

	for key := range inner.entries {
		require.NotContains(t, key, "..")
	}
}

func Test_TenantCache_MissPropagatesFromInner(t *testing.T) {
	inner := newMemCache()
	c := NewTenantCache(inner)

	ctx := contextWithTenant(t, "tenant-a")

	img, err := c.Lookup(ctx, testTileRequest())
	require.NoError(t, err)
	require.Nil(t, img)
}

func Test_TenantCache_ErrorsPropagate(t *testing.T) {
	c := NewTenantCache(erroringCache{})
	ctx := contextWithTenant(t, "tenant-a")

	_, err := c.Lookup(ctx, testTileRequest())
	require.Error(t, err)

	err = c.Save(ctx, testTileRequest(), &pkg.Image{Content: []byte("x")})
	require.Error(t, err)
}

func Test_TenantCache_Close_ForwardsToCloser(t *testing.T) {
	closed := false
	c := NewTenantCache(closerCache{closeFn: func() { closed = true }})

	require.NoError(t, c.Close(context.Background()))
	require.True(t, closed)
}

func Test_TenantRegistration_ConstructsWrappedCache(t *testing.T) {
	cfg := TenantConfig{
		Cache: map[string]interface{}{"name": "memory"},
	}

	c, err := TenantRegistration{}.Initialize(cfg, cache.CacheDeps{})
	require.NoError(t, err)

	tenantCache, ok := c.(*TenantCache)
	require.True(t, ok)

	ctx := contextWithTenant(t, "tenant-a")
	req := testTileRequest()
	img := pkg.Image{Content: []byte("data"), ContentType: "image/png"}
	require.NoError(t, tenantCache.Save(ctx, req, &img))

	out, err := tenantCache.Lookup(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, []byte("data"), out.Content)
}

func Test_TenantRegistration_PropagatesInnerCacheError(t *testing.T) {
	cfg := TenantConfig{Cache: map[string]interface{}{"name": "does-not-exist"}}

	_, err := TenantRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{EnumError: "invalid %v: %v not in %v"}})
	require.Error(t, err)
}

func Test_TenantRegistration_RegisteredUnderName(t *testing.T) {
	reg, ok := cache.RegisteredCache("tenant")
	require.True(t, ok)
	require.Equal(t, "tenant", reg.Name())
}
