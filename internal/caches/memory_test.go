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
	"time"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestMemory(t *testing.T) {
	cfg := MemoryConfig{}

	r, err := ConstructMemory(cfg, config.ErrorMessages{})
	assert.NoError(t, err)

	validateSaveAndLookup(t, r)
}

func TestTtl(t *testing.T) {
	cfg := MemoryConfig{Ttl: 1}

	r, err := ConstructMemory(cfg, config.ErrorMessages{})
	assert.NoError(t, err)

	tile := makeReq(53)
	img := makeImg(53)

	r.Save(tile, &img)

	if !validateLookup(t, r, tile, &img) {
		return
	}
	time.Sleep(time.Duration(2) * time.Second)
	validateNoLookup(t, r, tile)
}

//We intentionally don't test the maxsize property as the otter library doesn't offer guarantees on how capacity settings are honored.  See https://github.com/maypok86/otter/issues/88 for more details
