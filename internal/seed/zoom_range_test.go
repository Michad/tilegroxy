// Copyright 2026 Michael Davis
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

package seed

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSingleZoomRangeNormal(t *testing.T) {
	r, err := NewSingleZoomRange(pkg.Bounds{South: 40, North: 50, West: -10, East: 10, SRID: pkg.SRIDWGS84}, 4)

	require.NoError(t, err)
	assert.Equal(t, uint(4), r.Z)
	assert.Less(t, r.XMin, r.XMax)
	assert.Less(t, r.YMin, r.YMax)
	assert.Positive(t, r.Count())
}

func TestNewSingleZoomRangeInvalidZoom(t *testing.T) {
	_, err := NewSingleZoomRange(pkg.WorldBounds(), pkg.MaxZoom+1)

	require.Error(t, err)
	var rangeErr pkg.RangeError
	require.ErrorAs(t, err, &rangeErr)
}

func TestNewSingleZoomRangeAntimeridian(t *testing.T) {
	for _, b := range []pkg.Bounds{
		{South: 40, North: 50, West: 170, East: -170, SRID: pkg.SRIDWGS84},
		{South: 40, North: 50, West: 170, East: 190, SRID: pkg.SRIDWGS84},
	} {
		_, err := NewSingleZoomRange(b, 4)

		require.Error(t, err)
		var rangeErr pkg.RangeError
		require.ErrorAs(t, err, &rangeErr)
		assert.Equal(t, "east", rangeErr.ParamName)
		assert.InDelta(t, 170.0, rangeErr.MinValue, .0001)
		assert.InDelta(t, 180.0, rangeErr.MaxValue, .0001)
	}
}

func TestNewSingleZoomRangeInvertedLatitude(t *testing.T) {
	// Low zooms used to give a valid-looking range for the wrong band, high zooms underflowed Count
	for _, z := range []uint{2, 3, 5} {
		_, err := NewSingleZoomRange(pkg.Bounds{South: 60, North: -60, West: -180, East: 180, SRID: pkg.SRIDWGS84}, z)

		require.Error(t, err)
		var rangeErr pkg.RangeError
		require.ErrorAs(t, err, &rangeErr)
		assert.Equal(t, "north", rangeErr.ParamName)
		assert.InDelta(t, 60.0, rangeErr.MinValue, .0001)
	}
}

func TestNewSingleZoomRangeWorldDoesNotUnderflow(t *testing.T) {
	r, err := NewSingleZoomRange(pkg.WorldBounds(), 4)

	require.NoError(t, err)
	assert.Equal(t, uint64(16*16), r.Count())
}
