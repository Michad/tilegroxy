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
	"context"
	"errors"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMaxLength = 1 << 20

func openFixture(t *testing.T, name string) *Archive {
	t.Helper()

	src, err := OpenFile("testdata/" + name)
	require.NoError(t, err)

	a, err := Open(context.Background(), src, testMaxLength)
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Close() })

	return a
}

func Test_Open_FixtureMVT(t *testing.T) {
	a := openFixture(t, "vector.pmtiles")

	assert.Equal(t, TileTypeMVT, a.Header().TileType)
	assert.Equal(t, "tilegroxy-landmarks", a.Metadata()["name"])
	assert.Contains(t, a.Metadata(), "vector_layers")

	for _, tile := range []struct {
		z       uint8
		x, y    uint32
		length  int
		feature string
	}{{0, 0, 0, 51, "origin"}, {1, 1, 0, 54, "northeast"}} {
		data, ok, err := a.Tile(context.Background(), tile.z, tile.x, tile.y)
		require.NoError(t, err)
		require.True(t, ok, "%d/%d/%d", tile.z, tile.x, tile.y)
		assert.Len(t, data, tile.length)
		assert.Contains(t, string(data), "landmarks", "tile should be decompressed")
		assert.Contains(t, string(data), tile.feature)
	}

	_, ok, err := a.Tile(context.Background(), 1, 0, 0)
	require.NoError(t, err)
	assert.False(t, ok)
}

func Test_Open_FixturePNG(t *testing.T) {
	a := openFixture(t, "raster.pmtiles")

	assert.Equal(t, TileTypePNG, a.Header().TileType)
	assert.Equal(t, uint8(0), a.Header().Clustered)
	assert.Empty(t, a.Metadata())

	tiles := map[[2]uint32][]byte{}
	for _, xy := range [][2]uint32{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {0, 3}, {3, 0}} {
		data, ok, err := a.Tile(context.Background(), 2, xy[0], xy[1])
		require.NoError(t, err)
		require.True(t, ok, "2/%d/%d", xy[0], xy[1])
		assert.Len(t, data, 73)
		assert.Equal(t, []byte("\x89PNG"), data[:4])
		tiles[xy] = data
	}

	assert.Equal(t, tiles[[2]uint32{1, 0}], tiles[[2]uint32{0, 1}], "run length entry should repeat the tile")
	assert.Equal(t, tiles[[2]uint32{0, 0}], tiles[[2]uint32{3, 0}], "deduplicated tiles should share data")
	assert.NotEqual(t, tiles[[2]uint32{0, 0}], tiles[[2]uint32{1, 0}])
	assert.NotEqual(t, tiles[[2]uint32{0, 0}], tiles[[2]uint32{0, 3}])

	for _, xy := range [][2]uint32{{0, 2}, {1, 3}, {3, 3}} {
		_, ok, err := a.Tile(context.Background(), 2, xy[0], xy[1])
		require.NoError(t, err)
		assert.False(t, ok, "2/%d/%d", xy[0], xy[1])
	}
}

func Test_Tile_LeafDirectoriesAndGzip(t *testing.T) {
	src := &memSource{data: buildArchive(t, archiveOptions{
		tiles:               tilesAtZoom2(),
		tileType:            TileTypeMVT,
		internalCompression: CompressionGzip,
		tileCompression:     CompressionGzip,
		metadata:            `{"attribution":"test"}`,
		leafSize:            3,
	})}

	a, err := Open(context.Background(), src, testMaxLength)
	require.NoError(t, err)
	assert.Equal(t, "test", a.Metadata()["attribution"])

	for _, tile := range tilesAtZoom2() {
		data, ok, err := a.Tile(context.Background(), tile.z, tile.x, tile.y)
		require.NoError(t, err)
		require.True(t, ok, "%d/%d/%d", tile.z, tile.x, tile.y)
		assert.Equal(t, tile.data, data)
	}

	_, ok, err := a.Tile(context.Background(), 2, 0, 3)
	require.NoError(t, err)
	assert.False(t, ok)
}

func Test_Tile_LeafDirectoryIsCached(t *testing.T) {
	src := &memSource{data: buildArchive(t, archiveOptions{
		tiles:               tilesAtZoom2(),
		tileType:            TileTypeMVT,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `{}`,
		leafSize:            3,
	})}
	a, err := Open(context.Background(), src, testMaxLength)
	require.NoError(t, err)

	_, _, err = a.Tile(context.Background(), 2, 0, 0)
	require.NoError(t, err)
	before := src.reads.Load()

	_, _, err = a.Tile(context.Background(), 2, 0, 0)
	require.NoError(t, err)

	assert.Equal(t, before+1, src.reads.Load(), "second lookup should only read tile data")
}

func Test_Tile_OutsideZoomRange(t *testing.T) {
	a := openFixture(t, "raster.pmtiles")

	_, ok, err := a.Tile(context.Background(), 1, 0, 0)

	require.NoError(t, err)
	assert.False(t, ok)
}

func Test_Tile_ExceedsMaxLength(t *testing.T) {
	src := &memSource{data: buildArchive(t, archiveOptions{
		tiles:               []testTile{{z: 0, x: 0, y: 0, data: make([]byte, 200)}},
		tileType:            TileTypePNG,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `{}`,
	})}
	a, err := Open(context.Background(), src, 100)
	require.NoError(t, err)

	_, _, err = a.Tile(context.Background(), 0, 0, 0)

	require.ErrorIs(t, err, errSectionTooLarge)
}

func Test_Tile_TruncatedTileData(t *testing.T) {
	data := buildArchive(t, archiveOptions{
		tiles:               []testTile{{z: 0, x: 0, y: 0, data: []byte("abcdef")}},
		tileType:            TileTypePNG,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `{}`,
	})
	a, err := Open(context.Background(), &memSource{data: data[:len(data)-2]}, testMaxLength)
	require.NoError(t, err)

	_, _, err = a.Tile(context.Background(), 0, 0, 0)

	require.ErrorIs(t, err, errTruncated)
}

func Test_Tile_DirectoryDepthExceeded(t *testing.T) {
	// A leaf that points at itself forces endless descent
	var leaf []byte
	length := uint32(1)
	for {
		leaf = serializeEntries([]entry{{TileID: 0, Offset: 0, Length: length, RunLength: 0}})
		if uint32(len(leaf)) == length {
			break
		}
		length = uint32(len(leaf))
	}
	root := serializeEntries([]entry{{TileID: 0, Offset: 0, Length: length, RunLength: 0}})

	h := testHeader()
	h.MinZoom, h.MaxZoom = 0, 0
	h.RootOffset = HeaderLength
	h.RootLength = uint64(len(root))
	h.MetadataOffset = h.RootOffset + h.RootLength
	h.MetadataLength = 0
	h.LeafDirectoryOffset = h.MetadataOffset
	h.LeafDirectoryLength = uint64(len(leaf))
	h.TileDataOffset = h.LeafDirectoryOffset + h.LeafDirectoryLength

	data := append(serializeHeader(t, h), root...)
	data = append(data, leaf...)

	a, err := Open(context.Background(), &memSource{data: data}, testMaxLength)
	require.NoError(t, err)

	_, _, err = a.Tile(context.Background(), 0, 0, 0)

	require.ErrorIs(t, err, errTooDeep)
}

func Test_Open_Rejects(t *testing.T) {
	base := archiveOptions{
		tiles:               []testTile{{z: 0, x: 0, y: 0, data: []byte("x")}},
		tileType:            TileTypePNG,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `{}`,
	}

	cases := map[string]struct {
		mutate func(o *archiveOptions)
		target error
	}{
		"brotli tiles":     {func(o *archiveOptions) { o.tileCompression = CompressionBrotli }, errUnsupportedCompression},
		"zstd internal":    {func(o *archiveOptions) { o.internalCompression = CompressionZstd }, errUnsupportedCompression},
		"invalid metadata": {func(o *archiveOptions) { o.metadata = `{not json` }, nil},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			opts := base
			c.mutate(&opts)

			_, err := Open(context.Background(), &memSource{data: buildArchive(t, opts)}, testMaxLength)

			require.Error(t, err)
			if c.target != nil {
				require.ErrorIs(t, err, c.target)
			}
		})
	}
}

func Test_Open_AcceptsAnyTileType(t *testing.T) {
	for _, tt := range []TileType{TileTypeUnknown, TileTypeMLT, 99} {
		src := &memSource{data: buildArchive(t, archiveOptions{
			tiles:               []testTile{{z: 0, x: 0, y: 0, data: []byte("tile")}},
			tileType:            tt,
			internalCompression: CompressionNone,
			tileCompression:     CompressionNone,
			metadata:            `{}`,
		})}

		a, err := Open(context.Background(), src, testMaxLength)
		require.NoError(t, err, "type %d", tt)
		assert.Equal(t, tt, a.Header().TileType)
	}
}

func Test_Open_RejectsMaxZoomAbove31(t *testing.T) {
	h := testHeader()
	h.MaxZoom = 32

	_, err := Open(context.Background(), &memSource{data: serializeHeader(t, h)}, testMaxLength)

	require.ErrorIs(t, err, errZoomUnsupported)
}

func Test_Open_RejectsNonPositiveMaxLength(t *testing.T) {
	_, err := Open(context.Background(), &memSource{data: []byte{}}, 0)

	require.Error(t, err)
}

func Test_Open_RejectsNonArchive(t *testing.T) {
	_, err := Open(context.Background(), &memSource{data: []byte("definitely not an archive")}, testMaxLength)

	require.Error(t, err)
}

func Test_Open_RootOutsideFirstFetch(t *testing.T) {
	data := buildArchive(t, archiveOptions{
		tiles:               []testTile{{z: 0, x: 0, y: 0, data: []byte("tile")}},
		tileType:            TileTypePNG,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `{}`,
	})
	h, err := parseHeader(data)
	require.NoError(t, err)

	// Move the root directory past the 16 KiB initial fetch
	padding := make([]byte, 20000)
	root := data[h.RootOffset : h.RootOffset+h.RootLength]
	rest := data[h.RootOffset+h.RootLength:]
	h.MetadataOffset -= h.RootLength
	h.LeafDirectoryOffset -= h.RootLength
	h.TileDataOffset -= h.RootLength
	h.RootOffset = uint64(HeaderLength + len(rest) + len(padding))

	moved := append(serializeHeader(t, h), rest...)
	moved = append(moved, padding...)
	moved = append(moved, root...)

	a, err := Open(context.Background(), &memSource{data: moved}, testMaxLength)
	require.NoError(t, err)

	got, ok, err := a.Tile(context.Background(), 0, 0, 0)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []byte("tile"), got)
}

func Test_Open_RejectsWrappingRootOffset(t *testing.T) {
	h := testHeader()
	h.RootOffset = ^uint64(0) - 9 // 2^64-10
	h.RootLength = 20

	_, err := Open(context.Background(), &memSource{data: serializeHeader(t, h)}, testMaxLength)

	require.Error(t, err)
}

func Test_Open_RejectsWrappingMetadataOffset(t *testing.T) {
	root := serializeEntries(nil)

	h := testHeader()
	h.RootOffset = HeaderLength
	h.RootLength = uint64(len(root))
	h.MetadataOffset = ^uint64(0) - 9 // 2^64-10
	h.MetadataLength = 20

	data := append(serializeHeader(t, h), root...)

	_, err := Open(context.Background(), &memSource{data: data}, testMaxLength)

	require.Error(t, err)
}

func Test_Open_WrapsInitialReadError(t *testing.T) {
	_, err := Open(context.Background(), &errSource{err: errors.New("boom")}, testMaxLength)

	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
	require.ErrorContains(t, err, "pmtiles:")
}

func Test_Tile_WrapsReadErrorPreservesRemoteServerError(t *testing.T) {
	data := buildArchive(t, archiveOptions{
		tiles:               []testTile{{z: 0, x: 0, y: 0, data: []byte("x")}},
		tileType:            TileTypePNG,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `{}`,
	})
	h, err := parseHeader(data)
	require.NoError(t, err)

	src := &failAfterHeaderSource{data: data, failFrom: h.TileDataOffset}
	a, err := Open(context.Background(), src, testMaxLength)
	require.NoError(t, err)

	_, _, err = a.Tile(context.Background(), 0, 0, 0)

	require.Error(t, err)
	require.ErrorContains(t, err, "pmtiles:")
	var remoteErr pkg.RemoteServerError
	require.ErrorAs(t, err, &remoteErr)
	assert.Equal(t, 503, remoteErr.StatusCode)
}

func Test_Tile_OffsetOverflowDetected(t *testing.T) {
	root := serializeEntries([]entry{{TileID: 0, Offset: ^uint64(0) - 5, Length: 10, RunLength: 1}})

	h := testHeader()
	h.MinZoom, h.MaxZoom = 0, 0
	h.RootOffset = HeaderLength
	h.RootLength = uint64(len(root))
	h.MetadataLength = 0
	h.TileDataOffset = 100

	data := append(serializeHeader(t, h), root...)

	a, err := Open(context.Background(), &memSource{data: data}, testMaxLength)
	require.NoError(t, err)

	_, _, err = a.Tile(context.Background(), 0, 0, 0)

	require.ErrorIs(t, err, errInvalidOffset)
}

func Test_Tile_LeafOffsetOverflowDetected(t *testing.T) {
	root := serializeEntries([]entry{{TileID: 0, Offset: ^uint64(0) - 5, Length: 10, RunLength: 0}})

	h := testHeader()
	h.MinZoom, h.MaxZoom = 0, 0
	h.RootOffset = HeaderLength
	h.RootLength = uint64(len(root))
	h.MetadataLength = 0
	h.LeafDirectoryOffset = 100

	data := append(serializeHeader(t, h), root...)

	a, err := Open(context.Background(), &memSource{data: data}, testMaxLength)
	require.NoError(t, err)

	_, _, err = a.Tile(context.Background(), 0, 0, 0)

	require.ErrorIs(t, err, errInvalidOffset)
}

func Test_Open_NullMetadataBecomesEmptyMap(t *testing.T) {
	data := buildArchive(t, archiveOptions{
		tiles:               []testTile{{z: 0, x: 0, y: 0, data: []byte("x")}},
		tileType:            TileTypePNG,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `null`,
	})

	a, err := Open(context.Background(), &memSource{data: data}, testMaxLength)
	require.NoError(t, err)

	require.NotNil(t, a.Metadata())
	assert.Empty(t, a.Metadata())
}

func Test_Tile_HighBitsOfCoordinatesRejected(t *testing.T) {
	src := &memSource{data: buildArchive(t, archiveOptions{
		tiles:               []testTile{{z: 2, x: 0, y: 0, data: []byte("tile")}},
		tileType:            TileTypePNG,
		internalCompression: CompressionNone,
		tileCompression:     CompressionNone,
		metadata:            `{}`,
	})}
	a, err := Open(context.Background(), src, testMaxLength)
	require.NoError(t, err)

	// x=4 is 1<<z for z=2 and would alias to x=0 if high bits were ignored
	_, ok, err := a.Tile(context.Background(), 2, 4, 0)

	require.NoError(t, err)
	assert.False(t, ok)
}
