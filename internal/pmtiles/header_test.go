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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ParseHeader_RoundTrip(t *testing.T) {
	h := testHeader()
	b := serializeHeader(t, h)
	require.Len(t, b, HeaderLength)

	got, err := parseHeader(b)

	require.NoError(t, err)
	assert.Equal(t, h, got)
}

func Test_ParseHeader_IgnoresTrailingBytes(t *testing.T) {
	b := append(serializeHeader(t, testHeader()), 0xFF, 0xFF)

	_, err := parseHeader(b)

	require.NoError(t, err)
}

func Test_ParseHeader_BadMagic(t *testing.T) {
	b := serializeHeader(t, testHeader())
	b[0] = 'X'

	_, err := parseHeader(b)

	require.Error(t, err)
}

func Test_ParseHeader_WrongVersion(t *testing.T) {
	h := testHeader()
	h.Version = 2

	_, err := parseHeader(serializeHeader(t, h))

	require.Error(t, err)
}

func Test_ParseHeader_Truncated(t *testing.T) {
	_, err := parseHeader(serializeHeader(t, testHeader())[:100])

	require.Error(t, err)
}

func Test_Header_Bounds(t *testing.T) {
	assert.Equal(t, pkg.Bounds{South: -85, North: 85, West: -180, East: 180, SRID: pkg.SRIDWGS84}, testHeader().Bounds())
}

func Test_Header_Center(t *testing.T) {
	assert.Equal(t, []float64{1.5, -2.5, 2}, testHeader().Center())
}

func Test_TileType_ContentType(t *testing.T) {
	cases := map[TileType]string{
		TileTypeMVT:  "application/vnd.mapbox-vector-tile",
		TileTypePNG:  "image/png",
		TileTypeJPEG: "image/jpeg",
		TileTypeWebP: "image/webp",
		TileTypeAVIF: "image/avif",
	}
	for tt, want := range cases {
		got, ok := tt.ContentType()
		assert.True(t, ok)
		assert.Equal(t, want, got)
	}

	for _, tt := range []TileType{TileTypeUnknown, TileTypeMLT, 99} {
		_, ok := tt.ContentType()
		assert.False(t, ok, "type %d", tt)
	}
}
