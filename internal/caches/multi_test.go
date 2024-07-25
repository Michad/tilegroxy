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
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/require"
)

func TestMultiSaveAndLookup(t *testing.T) {
	memConfig1 := MemoryConfig{}

	mem1, err := MemoryRegistration{}.Initialize(memConfig1, config.ErrorMessages{})
	require.NoError(t, err)

	memConfig2 := MemoryConfig{}

	mem2, err := MemoryRegistration{}.Initialize(memConfig2, config.ErrorMessages{})
	require.NoError(t, err)

	multi := Multi{Tiers: []cache.Cache{mem1, mem2}}

	tile := makeReq(53)
	img := makeImg(24)
	require.NoError(t, multi.Save(tile, &img))

	validateLookup(t, multi, tile, &img)
	validateLookup(t, mem1, tile, &img)
	validateLookup(t, mem2, tile, &img)
}

func TestMultiIn1(t *testing.T) {
	memConfig1 := MemoryConfig{}

	mem1, err := MemoryRegistration{}.Initialize(memConfig1, config.ErrorMessages{})
	require.NoError(t, err)

	memConfig2 := MemoryConfig{}

	mem2, err := MemoryRegistration{}.Initialize(memConfig2, config.ErrorMessages{})
	require.NoError(t, err)

	multi := Multi{Tiers: []cache.Cache{mem1, mem2}}

	tile := makeReq(53)
	img := makeImg(24)
	require.NoError(t, mem1.Save(tile, &img))

	validateLookup(t, multi, tile, &img)
}

func TestMultiIn2(t *testing.T) {
	memConfig1 := MemoryConfig{}

	mem1, err := MemoryRegistration{}.Initialize(memConfig1, config.ErrorMessages{})
	require.NoError(t, err)

	memConfig2 := MemoryConfig{}

	mem2, err := MemoryRegistration{}.Initialize(memConfig2, config.ErrorMessages{})
	require.NoError(t, err)

	multi := Multi{Tiers: []cache.Cache{mem1, mem2}}

	tile := makeReq(53)
	img := makeImg(24)
	require.NoError(t, mem2.Save(tile, &img))

	validateLookup(t, multi, tile, &img)
}
