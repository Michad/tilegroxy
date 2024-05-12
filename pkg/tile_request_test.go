package pkg

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZoom0(t *testing.T) {
	r := TileRequest{"layer", 0, 0, 0}

	b, err := r.GetBounds()

	assert.Equal(t, nil, err)
	assert.InDelta(t, -85.0511, .0001, b.MinLat)
	assert.InDelta(t, 85.0511, .0001, b.MaxLat)
	assert.Equal(t, -180.0, b.MinLong)
	assert.Equal(t, 180.0, b.MaxLong)
}
func TestZoom8(t *testing.T) {
	r := TileRequest{"layer", 8, 132, 85}

	b, err := r.GetBounds()

	assert.Equal(t, nil, err)
	assert.InDelta(t, 50.736455, .0001, b.MinLat)
	assert.InDelta(t, 51.618016, .0001, b.MaxLat)
	assert.InDelta(t, 5.625000, .0001, b.MinLong)
	assert.InDelta(t, 7.031250, .0001, b.MaxLong)
}

func TestRangeError(t *testing.T) {
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
