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
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DeserializeEntries_RoundTrip(t *testing.T) {
	entries := []entry{
		{TileID: 0, Offset: 0, Length: 10, RunLength: 1},
		{TileID: 1, Offset: 10, Length: 20, RunLength: 2},
		{TileID: 5, Offset: 100, Length: 5, RunLength: 1},
		{TileID: 9, Offset: 0, Length: 50, RunLength: 0},
	}

	got, err := deserializeEntries(serializeEntries(entries))

	require.NoError(t, err)
	assert.Equal(t, entries, got)
}

func Test_DeserializeEntries_Empty(t *testing.T) {
	got, err := deserializeEntries(serializeEntries(nil))

	require.NoError(t, err)
	assert.Empty(t, got)
}

func Test_DeserializeEntries_Truncated(t *testing.T) {
	_, err := deserializeEntries([]byte{0x02, 0x01})

	require.Error(t, err)
}

func Test_DeserializeEntries_CountLargerThanData(t *testing.T) {
	_, err := deserializeEntries(binary.AppendUvarint(nil, 1<<40))

	require.Error(t, err)
}

func Test_DeserializeEntries_FirstOffsetZeroIsInvalid(t *testing.T) {
	b := []byte{0x01, 0x00, 0x01, 0x01, 0x00}

	_, err := deserializeEntries(b)

	require.Error(t, err)
}

func Test_DeserializeEntries_LengthOverflow(t *testing.T) {
	b := binary.AppendUvarint(nil, 1)
	b = binary.AppendUvarint(b, 0)
	b = binary.AppendUvarint(b, 1)
	b = binary.AppendUvarint(b, 1<<33)
	b = binary.AppendUvarint(b, 1)

	_, err := deserializeEntries(b)

	require.Error(t, err)
}

func Test_FindTile(t *testing.T) {
	entries := []entry{
		{TileID: 2, RunLength: 1},
		{TileID: 5, RunLength: 3},
		{TileID: 20, RunLength: 0},
	}

	cases := []struct {
		id     uint64
		found  bool
		wantID uint64
	}{
		{1, false, 0},
		{2, true, 2},
		{3, false, 0},
		{5, true, 5},
		{7, true, 5},
		{8, false, 0},
		{20, true, 20},
		{1000, true, 20},
	}

	for _, c := range cases {
		e, ok := findTile(entries, c.id)
		assert.Equal(t, c.found, ok, "id %d", c.id)
		if c.found {
			assert.Equal(t, c.wantID, e.TileID, "id %d", c.id)
		}
	}

	_, ok := findTile(nil, 0)
	assert.False(t, ok)
}
