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
