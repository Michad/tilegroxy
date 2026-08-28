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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/require"
)

func TestDisk(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)

	require.NoError(t, err)
	cfg := DiskConfig{Path: dir}

	c, err := DiskRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	validateSaveAndLookup(t, c)
}

// LayerName is User input for pattern layers, so a traversal sequence in it must not let
// Save/Lookup reach outside the configured cache directory.
func TestDisk_LayerNamePathTraversalIsContained(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	cfg := DiskConfig{Path: dir}
	cAny, err := DiskRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	c := cAny.(*Disk)

	maliciousTile := pkg.TileRequest{LayerName: "../../escaped", Z: 1, X: 2, Y: 3}
	img := pkg.Image{Content: []byte("payload")}

	err = c.Save(context.Background(), maliciousTile, &img)
	require.NoError(t, err)

	// Nothing should have been written outside the cache directory.
	_, statErr := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "escaped"))
	require.True(t, os.IsNotExist(statErr), "traversal payload escaped the cache directory")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one file written inside the cache directory")

	result, err := c.Lookup(context.Background(), maliciousTile)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, img.Content, result.Content)
}

// Disk has no native notion of expiry, so uniform per-layer TTL relies entirely on
// cache.TTLCache emulating it. This exercises that combination end to end against the real
// backend rather than a stub, covering both a hit inside the window and a miss past it.
func TestDisk_WrappedInTTLCache_ExpiresEntries(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk-ttl")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	cAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	ttl := cache.NewTTLCache(cAny, time.Hour)
	req := pkg.TileRequest{LayerName: "ttl", Z: 1, X: 2, Y: 3}
	img := pkg.Image{Content: []byte("fresh-tile"), ContentType: "image/png"}

	require.NoError(t, ttl.Save(context.Background(), req, &img))

	// Still within the window: a hit with the original content intact.
	result, err := ttl.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, img.Content, result.Content)
	require.Equal(t, img.ContentType, result.ContentType)

	// A separate TTLCache over the same on-disk entry but with a TTL shorter than the time that's
	// actually elapsed: reported as a miss so the caller regenerates and overwrites it.
	expiredView := cache.NewTTLCache(cAny, 0)
	expired, err := expiredView.Lookup(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, expired)
}

// A tile written directly by Disk.Save before TTL support existed (or by any path that bypasses
// TTLCache) has no envelope. Reading it through TTLCache must not panic and must still return the
// stored content rather than erroring out on the whole cache.
func TestDisk_WrappedInTTLCache_OldFormatEntryIsSafe(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk-ttl-legacy")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	cAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	disk := cAny.(*Disk)

	req := pkg.TileRequest{LayerName: "legacy", Z: 1, X: 2, Y: 3}
	legacyImg := pkg.Image{Content: []byte("pre-upgrade-tile"), ContentType: "image/png"}

	// Bypass TTLCache entirely, simulating a file written by a pre-TTL version of tilegroxy.
	require.NoError(t, disk.Save(context.Background(), req, &legacyImg))

	ttl := cache.NewTTLCache(cAny, time.Second)

	var result *pkg.Image
	require.NotPanics(t, func() {
		result, err = ttl.Lookup(context.Background(), req)
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, legacyImg.Content, result.Content)
}
