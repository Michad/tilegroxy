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

package pmtiles

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ZxyToID_KnownValues(t *testing.T) {
	cases := []struct {
		z    uint8
		x, y uint32
		id   uint64
	}{
		{0, 0, 0, 0},
		{1, 0, 0, 1},
		{1, 0, 1, 2},
		{1, 1, 1, 3},
		{1, 1, 0, 4},
		{2, 0, 0, 5},
		{12, 3423, 1763, 19078479},
	}

	for _, c := range cases {
		assert.Equal(t, c.id, ZxyToID(c.z, c.x, c.y), "%d/%d/%d", c.z, c.x, c.y)
	}
}

func Test_ZxyToID_UniqueAcrossZoom3(t *testing.T) {
	seen := map[uint64]bool{}
	for z := range uint8(4) {
		for x := range uint32(1 << z) {
			for y := range uint32(1 << z) {
				id := ZxyToID(z, x, y)
				assert.False(t, seen[id], "duplicate id %d", id)
				seen[id] = true
			}
		}
	}
	assert.Len(t, seen, 1+4+16+64)
}
