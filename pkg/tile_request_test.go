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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	require.Error(t, err)

	var re RangeError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "Y", re.ParamName)
	assert.InDelta(t, 0.0, re.MinValue, 0.00001)
	assert.InDelta(t, 3.0, re.MaxValue, 0.00001)
}

func TestBoundsIntersect(t *testing.T) {
	assert.True(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{.9, 1.1, 0.9, 1.1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{-1, 0.1, -1, 0.1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{0, 1, 0, 0.1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{0, 1, 0.9, 2, SRIDWGS84}))
	assert.True(t, Bounds{.9, 1.1, 0.9, 1.1, SRIDWGS84}.Intersects(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.True(t, Bounds{-1, 0.1, -1, 0.1, SRIDWGS84}.Intersects(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0, 0.1, SRIDWGS84}.Intersects(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0.9, 2, SRIDWGS84}.Intersects(Bounds{0, 1, 0, 1, SRIDWGS84}))

	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{101, 201, 10, 354, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{0, 1, 1, 2, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{0, 1, -1, 0, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{1, 2, 0, 1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{-1, 0, 0, 1, SRIDWGS84}))

	assert.True(t, Bounds{0, 10, 0, 10, SRIDWGS84}.Intersects(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{0, 10, 0, 10, SRIDWGS84}))
	assert.True(t, Bounds{0, 10, 0, 10, SRIDWGS84}.Intersects(Bounds{3, 4, 3, 4, SRIDWGS84}))
	assert.True(t, Bounds{3, 4, 3, 4, SRIDWGS84}.Intersects(Bounds{0, 10, 0, 10, SRIDWGS84}))

	assert.True(t, Bounds{-90, 90, -180, 180, SRIDWGS84}.Intersects(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Intersects(Bounds{-90, 90, -180, 180, SRIDWGS84}))
}

func TestBoundsContains(t *testing.T) {
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{.9, 1.1, 0.9, 1.1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{-1, 0.1, -1, 0.1, SRIDWGS84}))
	assert.True(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{0, 1, 0, 0.1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{0, 1, 0.9, 2, SRIDWGS84}))
	assert.False(t, Bounds{.9, 1.1, 0.9, 1.1, SRIDWGS84}.Contains(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.False(t, Bounds{-1, 0.1, -1, 0.1, SRIDWGS84}.Contains(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 0.1, SRIDWGS84}.Contains(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0.9, 2, SRIDWGS84}.Contains(Bounds{0, 1, 0, 1, SRIDWGS84}))

	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{101, 201, 10, 354, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{0, 1, 1, 2, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{0, 1, -1, 0, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{1, 2, 0, 1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{-1, 0, 0, 1, SRIDWGS84}))

	assert.True(t, Bounds{0, 10, 0, 10, SRIDWGS84}.Contains(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{0, 10, 0, 10, SRIDWGS84}))
	assert.True(t, Bounds{0, 10, 0, 10, SRIDWGS84}.Contains(Bounds{3, 4, 3, 4, SRIDWGS84}))
	assert.False(t, Bounds{3, 4, 3, 4, SRIDWGS84}.Contains(Bounds{0, 10, 0, 10, SRIDWGS84}))

	assert.True(t, Bounds{-90, 90, -180, 180, SRIDWGS84}.Contains(Bounds{0, 1, 0, 1, SRIDWGS84}))
	assert.False(t, Bounds{0, 1, 0, 1, SRIDWGS84}.Contains(Bounds{-90, 90, -180, 180, SRIDWGS84}))
}

func TestBoundsIntersectionWith(t *testing.T) {
	result := Bounds{0, 10, 0, 10, SRIDWGS84}.IntersectionWith(Bounds{5, 15, 5, 15, SRIDWGS84})
	assert.InDelta(t, 5.0, result.South, 0.00001)
	assert.InDelta(t, 10.0, result.North, 0.00001)
	assert.InDelta(t, 5.0, result.West, 0.00001)
	assert.InDelta(t, 10.0, result.East, 0.00001)
	assert.Equal(t, uint(SRIDWGS84), result.SRID)

	result = Bounds{0, 10, 0, 10, SRIDWGS84}.IntersectionWith(Bounds{2, 8, 2, 8, SRIDWGS84})
	assert.InDelta(t, 2.0, result.South, 0.00001)
	assert.InDelta(t, 8.0, result.North, 0.00001)
	assert.InDelta(t, 2.0, result.West, 0.00001)
	assert.InDelta(t, 8.0, result.East, 0.00001)
}

func TestWorldBounds(t *testing.T) {
	world := WorldBounds()

	assert.InDelta(t, -85.0511, world.South, 0.0001)
	assert.InDelta(t, 85.0511, world.North, 0.0001)
	assert.InDelta(t, -180.0, world.West, 0.0001)
	assert.InDelta(t, 180.0, world.East, 0.0001)
	assert.Equal(t, uint(SRIDWGS84), world.SRID)
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
	require.Error(t, err)
	assert.True(t, bbox.IsNullIsland())
}

func FuzzBoundsBufferRelative(f *testing.F) {
	f.Add(-1.0, 1.0, -1.0, 1.0, 0.5)
	f.Add(-1.0, 1.0, -1.0, 1.0, -0.5)
	f.Add(0.0, 10.0, 0.0, 10.0, 1.0)
	f.Add(0.0, 10.0, 0.0, 10.0, 10.0)

	f.Fuzz(func(t *testing.T, w, n, s, e, pct float64) {
		b := Bounds{West: w, East: e, North: n, South: s}
		b2 := b.BufferRelative(pct)
		assert.InDelta(t, b.Width()*(1+pct), b2.Width(), delta)
		assert.InDelta(t, b.Height()*(1+pct), b2.Height(), delta)
		cX, cY := b.Centroid()
		cX2, cY2 := b2.Centroid()
		assert.InDelta(t, cX, cX2, delta)
		assert.InDelta(t, cY, cY2, delta)
	})
}

func TestTileToEWKT(t *testing.T) {
	req := TileRequest{LayerName: "", Z: 2, X: 1, Y: 1}
	b, err := req.GetBoundsProjection(SRIDPsuedoMercator)
	require.NoError(t, err)
	// test case from result of postgis `SELECT ST_AsEWKT(ST_TileEnvelope(2,1,1))` with precision tweak
	assert.Equal(t, "SRID=3857;POLYGON((-10018754.1713945 0.0000000,-10018754.1713945 10018754.1713945,0.0000000 10018754.1713945,0.0000000 0.0000000,-10018754.1713945 0.0000000))", b.ToEWKT())
}
