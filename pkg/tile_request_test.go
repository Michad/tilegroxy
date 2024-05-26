package pkg

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBoundsToTileZoom0(t *testing.T) {
	b := Bounds{-90, 90, -180, 180}

	tilesArr, _ := b.FindTiles("test", 0, false)
	tiles := *tilesArr

	assert.Equal(t, 1, len(tiles))
	assert.Equal(t, "test", tiles[0].LayerName)
	assert.Equal(t, 0, tiles[0].X)
	assert.Equal(t, 0, tiles[0].Y)
	assert.Equal(t, 0, tiles[0].Z)
}
func TestBoundsToTileZoom1(t *testing.T) {
	b := Bounds{-90, 90, -180, 180}

	tilesArr, _ := b.FindTiles("test", 1, false)
	tiles := *tilesArr

	assert.Equal(t, 4, len(tiles))
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

	assert.Equal(t, 1, len(tiles))
	assert.Equal(t, "test", tiles[0].LayerName)
	assert.Equal(t, 132, tiles[0].X)
	assert.Equal(t, 85, tiles[0].Y)
	assert.Equal(t, 8, tiles[0].Z)
}

func TestTileToBoundsZoom0(t *testing.T) {
	r := TileRequest{"layer", 0, 0, 0}

	b, err := r.GetBounds()

	assert.Equal(t, nil, err)
	assert.InDelta(t, -85.0511, .0001, b.MinLat)
	assert.InDelta(t, 85.0511, .0001, b.MaxLat)
	assert.Equal(t, -180.0, b.MinLong)
	assert.Equal(t, 180.0, b.MaxLong)
}
func TestTileToBoundsZoom8(t *testing.T) {
	r := TileRequest{"layer", 8, 132, 85}

	b, err := r.GetBounds()

	assert.Equal(t, nil, err)
	assert.InDelta(t, 50.736455, .0001, b.MinLat)
	assert.InDelta(t, 51.618016, .0001, b.MaxLat)
	assert.InDelta(t, 5.625000, .0001, b.MinLong)
	assert.InDelta(t, 7.031250, .0001, b.MaxLong)
}

func TestTileRequestRangeError(t *testing.T) {
	r := TileRequest{"layer", 2, 0, 5}

	b, err := r.GetBounds()

	assert.Equal(t, (*Bounds)(nil), b)
	assert.NotEqual(t, nil, err)
	assert.IsType(t, RangeError{}, err)

	var re RangeError
	assert.True(t, errors.As(err, &re))
	assert.Equal(t, "Y", re.ParamName)
	assert.Equal(t, 0.0, re.MinValue)
	assert.Equal(t, 3.0, re.MaxValue)
}
