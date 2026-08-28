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

package seed

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collect(e *Enumeration, start uint64) ([]uint64, []pkg.TileRequest) {
	var indexes []uint64
	var tiles []pkg.TileRequest

	for i, tile := range e.From(start) {
		indexes = append(indexes, i)
		tiles = append(tiles, tile)
	}

	return indexes, tiles
}

func world() pkg.Bounds {
	return pkg.Bounds{South: -90, North: 90, West: -180, East: 180, SRID: pkg.SRIDWGS84}
}

func Test_Enumeration_Zoom0(t *testing.T) {
	e, err := NewEnumeration("test", world(), []uint{0})
	require.NoError(t, err)

	assert.Equal(t, uint64(1), e.Count())

	_, tiles := collect(e, 0)
	assert.Equal(t, []pkg.TileRequest{{LayerName: "test", Z: 0, X: 0, Y: 0}}, tiles)
}

// The count has to come from the bounds rather than from enumerating, since that's what the size
// guard is based on for runs far too big to enumerate.
func Test_Enumeration_CountMatchesWhatIsYielded(t *testing.T) {
	e, err := NewEnumeration("test", world(), []uint{0, 1, 2, 3})
	require.NoError(t, err)

	assert.Equal(t, uint64(1+4+16+64), e.Count())

	_, tiles := collect(e, 0)
	assert.Len(t, tiles, int(e.Count()))
}

// Resuming from a position is only meaningful if the same arguments always produce the same
// sequence, including when the zoom flags are given out of order or repeated.
func Test_Enumeration_OrderIsDeterministic(t *testing.T) {
	e, err := NewEnumeration("test", world(), []uint{0, 1, 2})
	require.NoError(t, err)

	indexes, tiles := collect(e, 0)

	assert.Equal(t, []uint64{0, 1, 2, 3, 4}, indexes[0:5])
	assert.Equal(t, []pkg.TileRequest{
		{LayerName: "test", Z: 0, X: 0, Y: 0},
		{LayerName: "test", Z: 1, X: 0, Y: 0},
		{LayerName: "test", Z: 1, X: 0, Y: 1},
		{LayerName: "test", Z: 1, X: 1, Y: 0},
		{LayerName: "test", Z: 1, X: 1, Y: 1},
	}, tiles[0:5])

	shuffled, err := NewEnumeration("test", world(), []uint{2, 1, 0, 1})
	require.NoError(t, err)

	_, shuffledTiles := collect(shuffled, 0)
	assert.Equal(t, tiles, shuffledTiles)
	assert.Equal(t, e.Zooms(), shuffled.Zooms())
}

func Test_Enumeration_RestrictiveBounds(t *testing.T) {
	e, err := NewEnumeration("test", pkg.Bounds{South: 51, North: 51.6, West: 5.7, East: 7.0, SRID: pkg.SRIDWGS84}, []uint{8})
	require.NoError(t, err)

	assert.Equal(t, uint64(1), e.Count())

	_, tiles := collect(e, 0)
	assert.Equal(t, []pkg.TileRequest{{LayerName: "test", Z: 8, X: 132, Y: 85}}, tiles)

	// The same area at a coarser zoom is still a subset of the whole world.
	coarse, err := NewEnumeration("test", pkg.Bounds{South: 51, North: 51.6, West: 5.7, East: 7.0, SRID: pkg.SRIDWGS84}, []uint{4})
	require.NoError(t, err)
	assert.Less(t, coarse.Count(), uint64(256))
}

// From is what makes resuming work, so its first tile has to be exactly the one at the given
// position, not one either side of it.
func Test_Enumeration_FromResumesAtExactPosition(t *testing.T) {
	e, err := NewEnumeration("test", world(), []uint{0, 1, 2})
	require.NoError(t, err)

	_, all := collect(e, 0)

	for _, start := range []uint64{0, 1, 3, 5, 20, e.Count() - 1} {
		indexes, tiles := collect(e, start)

		assert.Equal(t, start, indexes[0], "resuming at %v", start)
		assert.Equal(t, all[start], tiles[0], "resuming at %v", start)
		assert.Len(t, tiles, int(e.Count()-start), "resuming at %v", start)
	}
}

func Test_Enumeration_FromAtEndYieldsNothing(t *testing.T) {
	e, err := NewEnumeration("test", world(), []uint{0, 1})
	require.NoError(t, err)

	_, tiles := collect(e, e.Count())
	assert.Empty(t, tiles)
}

// Whole zoom levels below the resume point are skipped without walking each tile, so the skipping
// has to leave the index aligned with the full sequence.
func Test_Enumeration_FromSkipsWholeZoomLevels(t *testing.T) {
	e, err := NewEnumeration("test", world(), []uint{0, 1, 2})
	require.NoError(t, err)

	indexes, tiles := collect(e, 5)

	assert.Equal(t, uint64(5), indexes[0])
	assert.Equal(t, pkg.TileRequest{LayerName: "test", Z: 2, X: 0, Y: 0}, tiles[0])
}

func Test_Enumeration_StopsEarlyWhenCallerBreaks(t *testing.T) {
	e, err := NewEnumeration("test", world(), []uint{0, 1, 2})
	require.NoError(t, err)

	count := 0

	for range e.All() {
		count++
		if count == 3 {
			break
		}
	}

	assert.Equal(t, 3, count)
}

func Test_Enumeration_InvalidZoom(t *testing.T) {
	_, err := NewEnumeration("test", world(), []uint{pkg.MaxZoom + 1})
	require.Error(t, err)
}
