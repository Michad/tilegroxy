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
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

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
	validateRemove(t, c)
}

func TestDisk_CoordinateLayout(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	cAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir, Layout: DiskLayoutCoordinate}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	validateSaveAndLookup(t, cAny)
	validateRemove(t, cAny)

	c := cAny.(*Disk)
	tile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}
	img := pkg.Image{Content: []byte("payload")}
	require.NoError(t, c.Save(context.Background(), tile, &img))

	_, err = os.Stat(filepath.Join(dir, "layer", "1", "2", "3"))
	require.NoError(t, err, "tile should be written to layer/z/x/y")

	// No stray temp files should survive alongside the tile.
	entries, err := os.ReadDir(filepath.Join(dir, "layer", "1", "2"))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	result, err := c.Lookup(context.Background(), tile)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, img.Content, result.Content)

	removed, err := c.Remove(context.Background(), tile)
	require.NoError(t, err)
	require.True(t, removed)

	result, err = c.Lookup(context.Background(), tile)
	require.NoError(t, err)
	require.Nil(t, result)
}

// The two layouts use different paths, so an entry written by one must not be found by the other.
func TestDisk_LayoutsDoNotShareEntries(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	deps := cache.CacheDeps{ErrorMessages: config.ErrorMessages{}}
	flatAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir}, deps)
	require.NoError(t, err)
	coordAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir, Layout: DiskLayoutCoordinate}, deps)
	require.NoError(t, err)

	tile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}
	require.NoError(t, flatAny.Save(context.Background(), tile, &pkg.Image{Content: []byte("flat")}))

	result, err := coordAny.Lookup(context.Background(), tile)
	require.NoError(t, err)
	require.Nil(t, result)
}

// LayerName is User input, so a traversal sequence must not escape the tree in coordinate layout either.
func TestDisk_CoordinateLayoutPathTraversalIsContained(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	cAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir, Layout: DiskLayoutCoordinate}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	c := cAny.(*Disk)

	maliciousTile := pkg.TileRequest{LayerName: "../../escaped", Z: 1, X: 2, Y: 3}
	img := pkg.Image{Content: []byte("payload")}
	require.NoError(t, c.Save(context.Background(), maliciousTile, &img))

	_, statErr := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "escaped"))
	require.True(t, os.IsNotExist(statErr), "traversal payload escaped the cache directory")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one directory written inside the cache directory")

	result, err := c.Lookup(context.Background(), maliciousTile)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, img.Content, result.Content)
}

func TestDisk_InvalidLayout(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	cfg := DiskConfig{Path: dir, Layout: "nonsense"}
	_, err = DiskRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{EnumError: "invalid %v: %v not in %v"}})
	require.Error(t, err)
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

// A 0-byte file left by an interrupted write must read as a miss rather than an empty tile.
func TestDisk_TruncatedFileIsAMiss(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)
	require.NoError(t, err)

	cAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	c := cAny.(*Disk)

	tile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}
	require.NoError(t, os.WriteFile(filepath.Join(dir, requestToFilename(tile)), []byte{}, 0600))

	result, err := c.Lookup(context.Background(), tile)
	require.NoError(t, err)
	require.Nil(t, result)

	// The miss lets a subsequent save replace the truncated entry.
	img := pkg.Image{Content: []byte("payload")}
	require.NoError(t, c.Save(context.Background(), tile, &img))

	result, err = c.Lookup(context.Background(), tile)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, img.Content, result.Content)
}

func newGzipDisk(t *testing.T, types ...string) (*Disk, string) {
	t.Helper()
	dir := t.TempDir()

	cAny, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir, Gzip: types}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	return cAny.(*Disk), dir
}

func readEntry(t *testing.T, dir string, tile pkg.TileRequest) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, requestToFilename(tile)))
	require.NoError(t, err)

	return b
}

func TestDisk_Gzip(t *testing.T) {
	c, dir := newGzipDisk(t, "application/vnd.mapbox-vector-tile", "text/plain")
	validateSaveAndLookup(t, c)
	validateRemove(t, c)

	tile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}
	img := pkg.Image{Content: bytes.Repeat([]byte("vector"), 100), ContentType: "application/vnd.mapbox-vector-tile"}
	require.NoError(t, c.Save(context.Background(), tile, &img))
	require.True(t, bytes.HasPrefix(readEntry(t, dir, tile), gzipMagic))

	result, err := c.Lookup(context.Background(), tile)
	require.NoError(t, err)
	require.Equal(t, img.Content, result.Content)
	require.Equal(t, img.ContentType, result.ContentType)

	// Content type parameters are ignored and a missing content type is sniffed.
	for _, ct := range []string{"text/plain; charset=utf-8", ""} {
		img = pkg.Image{Content: []byte("hello world"), ContentType: ct}
		require.NoError(t, c.Save(context.Background(), tile, &img))
		require.True(t, bytes.HasPrefix(readEntry(t, dir, tile), gzipMagic), ct)
	}
}

func TestDisk_GzipSkipsOtherContent(t *testing.T) {
	c, dir := newGzipDisk(t, "application/vnd.mapbox-vector-tile")
	tile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}

	cases := []pkg.Image{
		{Content: []byte("png"), ContentType: "image/png"},
		{Content: []byte("bad"), ContentType: "not a;;; type"},
		{Content: append([]byte{0x1f, 0x8b}, []byte("already compressed")...), ContentType: "application/vnd.mapbox-vector-tile"},
	}

	for _, img := range cases {
		require.NoError(t, c.Save(context.Background(), tile, &img))
		require.False(t, bytes.HasPrefix(readEntry(t, dir, tile), gzipMagic), img.ContentType)

		result, err := c.Lookup(context.Background(), tile)
		require.NoError(t, err)
		require.Equal(t, img.Content, result.Content)
	}
}

// Toggling gzip must not require purging the cache directory.
func TestDisk_GzipToggleKeepsEntriesReadable(t *testing.T) {
	c, dir := newGzipDisk(t, "text/plain")
	plain, err := DiskRegistration{}.Initialize(DiskConfig{Path: dir}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	gzTile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 1, Y: 1}
	plainTile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 1, Y: 2}
	img := pkg.Image{Content: []byte("text"), ContentType: "text/plain"}
	require.NoError(t, c.Save(context.Background(), gzTile, &img))
	require.NoError(t, plain.Save(context.Background(), plainTile, &img))

	for _, reader := range []cache.Cache{c, plain} {
		for _, tile := range []pkg.TileRequest{gzTile, plainTile} {
			result, err := reader.Lookup(context.Background(), tile)
			require.NoError(t, err)
			require.Equal(t, img.Content, result.Content)
		}
	}
}

// Legacy raw entries hold the tile bytes directly, which may themselves be gzip.
func TestDisk_GzipLegacyRawEntry(t *testing.T) {
	c, dir := newGzipDisk(t, "text/plain")
	tile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write([]byte("raw tile"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	for _, raw := range [][]byte{buf.Bytes(), {0x1f, 0x8b, 0x00}, buf.Bytes()[:buf.Len()-4]} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, requestToFilename(tile)), raw, 0600))

		result, err := c.Lookup(context.Background(), tile)
		require.NoError(t, err)
		require.Equal(t, raw, result.Content)
	}
}

func TestDisk_GzipBomb(t *testing.T) {
	c, dir := newGzipDisk(t, "text/plain")
	tile := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}

	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	require.NoError(t, err)
	_, err = w.Write(make([]byte, diskGzipMaxSize+1))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, os.WriteFile(filepath.Join(dir, requestToFilename(tile)), buf.Bytes(), 0600))

	_, err = c.Lookup(context.Background(), tile)
	require.Error(t, err)
}

func TestDisk_GzipInvalidContentType(t *testing.T) {
	_, err := DiskRegistration{}.Initialize(DiskConfig{Path: t.TempDir(), Gzip: []string{"not a;;; type"}}, cache.CacheDeps{ErrorMessages: config.ErrorMessages{InvalidParam: "invalid %v: %v"}})
	require.Error(t, err)
}
