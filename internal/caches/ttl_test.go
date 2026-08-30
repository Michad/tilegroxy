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
	"errors"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/require"
)

// memCache is a minimal in-memory Cache used to test TTLCache without depending on any real
// backend. It stores whatever *pkg.Image TTLCache hands it verbatim, the same way the real
// backends store whatever bytes/Image they're given.
type memCache struct {
	entries map[string]*pkg.Image
}

func newMemCache() *memCache {
	return &memCache{entries: make(map[string]*pkg.Image)}
}

func (m *memCache) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	img, ok := m.entries[t.String()]
	if !ok {
		return nil, nil
	}
	return img, nil
}

func (m *memCache) Save(_ context.Context, t pkg.TileRequest, img *pkg.Image) error {
	m.entries[t.String()] = img
	return nil
}

func testTileRequest() pkg.TileRequest {
	return pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}
}

func Test_TTLCache_HitWithinWindow(t *testing.T) {
	inner := newMemCache()
	now := time.Now()
	c := NewTTLCache(inner, time.Hour)
	c.clock = func() time.Time { return now }

	req := testTileRequest()
	require.NoError(t, c.Save(context.Background(), req, &pkg.Image{Content: []byte("data"), ContentType: "image/png"}))

	// Still within the TTL window
	c.clock = func() time.Time { return now.Add(30 * time.Minute) }

	img, err := c.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, []byte("data"), img.Content)
	require.Equal(t, "image/png", img.ContentType)
}

func Test_TTLCache_MissAfterExpiry(t *testing.T) {
	inner := newMemCache()
	now := time.Now()
	c := NewTTLCache(inner, time.Hour)
	c.clock = func() time.Time { return now }

	req := testTileRequest()
	require.NoError(t, c.Save(context.Background(), req, &pkg.Image{Content: []byte("data"), ContentType: "image/png"}))

	// Past the TTL window: should be reported as a miss so the caller regenerates and overwrites it
	c.clock = func() time.Time { return now.Add(2 * time.Hour) }

	img, err := c.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, img)

	// The underlying entry is still physically present - TTLCache never deletes, it just reports
	// a miss so the normal request path overwrites it on next save.
	require.NotNil(t, inner.entries[req.String()])
}

func Test_TTLCache_OldFormatEntryIsSafeAndTreatedAsHit(t *testing.T) {
	inner := newMemCache()
	req := testTileRequest()

	// Simulate data written before TTL support existed / by a path that doesn't go through
	// TTLCache: raw bytes with no envelope at all.
	inner.entries[req.String()] = &pkg.Image{Content: []byte("legacy-data"), ContentType: "image/png"}

	c := NewTTLCache(inner, time.Second)

	img, err := c.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, []byte("legacy-data"), img.Content)
}

func Test_TTLCache_MissPropagatesFromInner(t *testing.T) {
	inner := newMemCache()
	c := NewTTLCache(inner, time.Hour)

	img, err := c.Lookup(context.Background(), testTileRequest())
	require.NoError(t, err)
	require.Nil(t, img)
}

type erroringCache struct{}

func (erroringCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, errIntentional
}
func (erroringCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return errIntentional
}

var errIntentional = errors.New("intentional test error")

func Test_TTLCache_ErrorsPropagate(t *testing.T) {
	c := NewTTLCache(erroringCache{}, time.Hour)

	_, err := c.Lookup(context.Background(), testTileRequest())
	require.Error(t, err)

	err = c.Save(context.Background(), testTileRequest(), &pkg.Image{Content: []byte("x")})
	require.Error(t, err)
}

func Test_TTLCache_Close_ForwardsToCloser(t *testing.T) {
	closed := false
	c := NewTTLCache(closerCache{closeFn: func() { closed = true }}, time.Hour)

	require.NoError(t, c.Close(context.Background()))
	require.True(t, closed)
}

type closerCache struct {
	closeFn func()
}

func (closerCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) { return nil, nil }
func (closerCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error   { return nil }
func (c closerCache) Close(_ context.Context) error {
	c.closeFn()
	return nil
}

func Test_TTLRegistration_ConstructsWrappedCache(t *testing.T) {
	config := TTLConfig{
		TTL:   60,
		Cache: map[string]interface{}{"name": "memory"},
	}

	c, err := TTLRegistration{}.Initialize(config, cache.CacheDeps{})
	require.NoError(t, err)

	ttlCache, ok := c.(*TTLCache)
	require.True(t, ok)
	require.Equal(t, time.Minute, ttlCache.TTL)

	req := testTileRequest()
	img := pkg.Image{Content: []byte("data"), ContentType: "image/png"}
	require.NoError(t, ttlCache.Save(context.Background(), req, &img))

	out, err := ttlCache.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, []byte("data"), out.Content)
}

func Test_TTLRegistration_RequiresTTL(t *testing.T) {
	cfg := TTLConfig{Cache: map[string]interface{}{"name": "memory"}}

	_, err := TTLRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{InvalidParam: "invalid %v: %v"}})
	require.Error(t, err)
}

func Test_TTLRegistration_PropagatesInnerCacheError(t *testing.T) {
	cfg := TTLConfig{TTL: 60, Cache: map[string]interface{}{"name": "does-not-exist"}}

	_, err := TTLRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{EnumError: "invalid %v: %v not in %v"}})
	require.Error(t, err)
}

func Test_TTLRegistration_RegisteredUnderName(t *testing.T) {
	reg, ok := cache.RegisteredCache("ttl")
	require.True(t, ok)
	require.Equal(t, "ttl", reg.Name())
}
