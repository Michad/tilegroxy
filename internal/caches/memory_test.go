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
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/require"
)

func TestMemory(t *testing.T) {
	cfg := MemoryConfig{}

	r, err := MemoryRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	validateSaveAndLookup(t, r)
}

func TestTtl(t *testing.T) {
	cfg := MemoryConfig{TTL: 1}

	r, err := MemoryRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	tile := makeReq(53)
	img := makeImg(53)

	require.NoError(t, r.Save(context.Background(), tile, &img))

	validateLookup(t, r, tile, &img)
	time.Sleep(time.Duration(2) * time.Second)
	validateNoLookup(t, r, tile)
}

// We intentionally don't test the maxsize property as the otter library doesn't offer guarantees on how capacity settings are honored.  See https://github.com/maypok86/otter/issues/88 for more details

func TestPurgeDeletesOnlyMatchingLayer(t *testing.T) {
	cfg := MemoryConfig{}

	r, err := MemoryRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	tileA := pkg.TileRequest{LayerName: "a", Z: 1, X: 2, Y: 3}
	tileB := pkg.TileRequest{LayerName: "b", Z: 1, X: 2, Y: 3}
	imgA := makeImg(1)
	imgB := makeImg(2)

	require.NoError(t, r.Save(context.Background(), tileA, &imgA))
	require.NoError(t, r.Save(context.Background(), tileB, &imgB))

	purgeable, ok := r.(cache.Purgeable)
	require.True(t, ok, "Memory cache should implement cache.Purgeable")

	require.NoError(t, purgeable.Purge(context.Background(), "a"))

	validateNoLookup(t, r, tileA)
	validateLookup(t, r, tileB, &imgB)
}

func TestPurgeOfUnknownLayerIsANoOp(t *testing.T) {
	cfg := MemoryConfig{}

	r, err := MemoryRegistration{}.Initialize(cfg, cache.CacheDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	tile := makeReq(11)
	img := makeImg(11)
	require.NoError(t, r.Save(context.Background(), tile, &img))

	purgeable, ok := r.(cache.Purgeable)
	require.True(t, ok)

	require.NoError(t, purgeable.Purge(context.Background(), "does-not-exist"))

	validateLookup(t, r, tile, &img)
}
