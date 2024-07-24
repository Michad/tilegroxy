// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package pkg

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundsToTileZoom0(t *testing.T) {
	b := Bounds{-90, 90, -180, 180}

	tilesArr, _ := b.FindTiles("test", 0, false)
	tiles := *tilesArr

	assert.Len(t, tiles, 1)
	assert.Equal(t, "test", tiles[0].LayerName)
	assert.Equal(t, 0, tiles[0].X)
	assert.Equal(t, 0, tiles[0].Y)
	assert.Equal(t, 0, tiles[0].Z)
}
func TestBoundsToTileZoom1(t *testing.T) {
	b := Bounds{-90, 90, -180, 180}

	tilesArr, _ := b.FindTiles("test", 1, false)
	tiles := *tilesArr

	assert.Len(t, tiles, 4)
	assert.Equal(t, "test", tiles[0].LayerName)
	assert.Equal(t, 1, tiles[0].Z)

	for _, tile := range tiles {
		assert.LessOrEqual(t, 0, tile.X)
		assert.LessOrEqual(t, 0, tile.Y)
		assert.GreaterOrEqual(t, 1, tile.X)
		assert.GreaterOrEqual(t, 1, tile.Y)
	}
}

func TestBoundsToTileZoom8(t *testing.T) {
	b := Bounds{51, 51.6, 5.7, 7.0}

	tilesArr, _ := b.FindTiles("test", 8, false)
	tiles := *tilesArr

	assert.Len(t, tiles, 1)
	assert.Equal(t, "test", tiles[0].LayerName)
	assert.Equal(t, 132, tiles[0].X)
	assert.Equal(t, 85, tiles[0].Y)
	assert.Equal(t, 8, tiles[0].Z)
}

func TestTileToBoundsZoom0(t *testing.T) {
	r := TileRequest{"layer", 0, 0, 0}

	b, err := r.GetBounds()

	require.NoError(t, err)
	assert.InDelta(t, -85.0511, b.South, .0001)
	assert.InDelta(t, 85.0511, b.North, .0001)
	assert.InDelta(t, -180.0, b.West, .0001)
	assert.InDelta(t, 180.0, b.East, .0001)
}
func TestTileToBoundsZoom8(t *testing.T) {
	r := TileRequest{"layer", 8, 132, 85}

	b, err := r.GetBounds()

	require.NoError(t, err)
	assert.InDelta(t, 50.736455, .0001, b.South)
	assert.InDelta(t, 51.618016, .0001, b.North)
	assert.InDelta(t, 5.625000, .0001, b.West)
	assert.InDelta(t, 7.031250, .0001, b.East)
}

func TestTileRequestRangeError(t *testing.T) {
	r := TileRequest{"layer", 2, 0, 5}

	b, err := r.GetBounds()

	assert.Equal(t, (*Bounds)(nil), b)
	require.NoError(t, err)
	assert.IsType(t, RangeError{}, err)

	var re RangeError
	assert.ErrorAs(t, err, &re)
	assert.Equal(t, "Y", re.ParamName)
	assert.InDelta(t, 0.0, re.MinValue, 0.00001)
	assert.InDelta(t, 3.0, re.MaxValue, 0.00001)
}

func TestBoundsIntersect(t *testing.T) {
	assert.True(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{.9, 1.1, 0.9, 1.1}))
	assert.True(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{-1, 0.1, -1, 0.1}))
	assert.True(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{0, 1, 0, 0.1}))
	assert.True(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{0, 1, 0.9, 2}))
	assert.True(t, Bounds{.9, 1.1, 0.9, 1.1}.Intersects(Bounds{0, 1, 0, 1}))
	assert.True(t, Bounds{-1, 0.1, -1, 0.1}.Intersects(Bounds{0, 1, 0, 1}))
	assert.True(t, Bounds{0, 1, 0, 0.1}.Intersects(Bounds{0, 1, 0, 1}))
	assert.True(t, Bounds{0, 1, 0.9, 2}.Intersects(Bounds{0, 1, 0, 1}))

	assert.False(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{101, 200, 10, 354}))
	assert.False(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{0, 1, 1, 2}))
	assert.False(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{0, 1, -1, 0}))
	assert.False(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{1, 2, 0, 1}))
	assert.False(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{-1, 0, 0, 1}))

	assert.True(t, Bounds{0, 10, 0, 10}.Intersects(Bounds{0, 1, 0, 1}))
	assert.True(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{0, 10, 0, 10}))
	assert.True(t, Bounds{0, 10, 0, 10}.Intersects(Bounds{3, 4, 3, 4}))
	assert.True(t, Bounds{3, 4, 3, 4}.Intersects(Bounds{0, 10, 0, 10}))

	assert.True(t, Bounds{-90, 90, -180, 180}.Intersects(Bounds{0, 1, 0, 1}))
	assert.True(t, Bounds{0, 1, 0, 1}.Intersects(Bounds{-90, 90, -180, 180}))
}

func TestBoundsContains(t *testing.T) {
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{.9, 1.1, 0.9, 1.1}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{-1, 0.1, -1, 0.1}))
	assert.True(t, Bounds{0, 1, 0, 1}.Contains(Bounds{0, 1, 0, 0.1}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{0, 1, 0.9, 2}))
	assert.False(t, Bounds{.9, 1.1, 0.9, 1.1}.Contains(Bounds{0, 1, 0, 1}))
	assert.False(t, Bounds{-1, 0.1, -1, 0.1}.Contains(Bounds{0, 1, 0, 1}))
	assert.False(t, Bounds{0, 1, 0, 0.1}.Contains(Bounds{0, 1, 0, 1}))
	assert.False(t, Bounds{0, 1, 0.9, 2}.Contains(Bounds{0, 1, 0, 1}))

	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{101, 200, 10, 354}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{0, 1, 1, 2}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{0, 1, -1, 0}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{1, 2, 0, 1}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{-1, 0, 0, 1}))

	assert.True(t, Bounds{0, 10, 0, 10}.Contains(Bounds{0, 1, 0, 1}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{0, 10, 0, 10}))
	assert.True(t, Bounds{0, 10, 0, 10}.Contains(Bounds{3, 4, 3, 4}))
	assert.False(t, Bounds{3, 4, 3, 4}.Contains(Bounds{0, 10, 0, 10}))

	assert.True(t, Bounds{-90, 90, -180, 180}.Contains(Bounds{0, 1, 0, 1}))
	assert.False(t, Bounds{0, 1, 0, 1}.Contains(Bounds{-90, 90, -180, 180}))
}

func TestGeohashToBounds(t *testing.T) {
	bbox, err := NewBoundsFromGeohash("gbsuv7z")

	require.NoError(t, err)
	assert.InDelta(t, -4.329986572265625, bbox.West, 0.000001)
	assert.InDelta(t, -4.32861328125, bbox.East, 0.000001)
	assert.InDelta(t, 48.66943359375, bbox.North, 0.000001)
	assert.InDelta(t, 48.668060302734375, bbox.South, 0.000001)

	bbox, err = NewBoundsFromGeohash("gb")

	require.NoError(t, err)
	assert.InDelta(t, -11.25, bbox.West, 0.000001)
	assert.InDelta(t, 0, bbox.East, 0.000001)
	assert.InDelta(t, 50.625, bbox.North, 0.000001)
	assert.InDelta(t, 45, bbox.South, 0.000001)

	bbox, err = NewBoundsFromGeohash("some nonsense")
	require.NoError(t, err)
	assert.True(t, bbox.IsNullIsland())
}

// Test converting a tile to bounds and back is an identity function within reason
func FuzzToBoundsAndBack(f *testing.F) {

	for z := 1; z < 21; z++ {
		f.Add(z, int(math.Exp2(float64(z))/2), int(math.Exp2(float64(z))/2))
	}
	f.Fuzz(func(t *testing.T, z int, x int, y int) {
		orig := TileRequest{"layer", z, x, y}
		b, err := orig.GetBounds()
		require.NoError(t, err)

		// Small delta to avoid floating point rounding errors causing an extra tile
		b.West += 0.000000001
		b.South += 0.000000001
		b.East -= 0.000000001
		b.North -= 0.000000001

		newTiles, err := b.FindTiles(orig.LayerName, uint(orig.Z), false)

		assert.Nil(t, err, "Error getting tiles for %v at %v", b, orig.Z)
		require.Len(t, *newTiles, 1)
		assert.Equal(t, orig, (*newTiles)[0])
	})
}
