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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/require"
)

func serializeEntries(entries []entry) []byte {
	b := binary.AppendUvarint(nil, uint64(len(entries)))

	var last uint64
	for _, e := range entries {
		b = binary.AppendUvarint(b, e.TileID-last)
		last = e.TileID
	}
	for _, e := range entries {
		b = binary.AppendUvarint(b, uint64(e.RunLength))
	}
	for _, e := range entries {
		b = binary.AppendUvarint(b, uint64(e.Length))
	}
	for i, e := range entries {
		if i > 0 && e.Offset == entries[i-1].Offset+uint64(entries[i-1].Length) {
			b = binary.AppendUvarint(b, 0)
		} else {
			b = binary.AppendUvarint(b, e.Offset+1)
		}
	}

	return b
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(b)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return buf.Bytes()
}

func serializeHeader(t *testing.T, h Header) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.LittleEndian, h))

	return buf.Bytes()
}

func testHeader() Header {
	return Header{
		Magic:               headerMagic,
		Version:             3,
		RootOffset:          127,
		RootLength:          10,
		MetadataOffset:      137,
		MetadataLength:      2,
		LeafDirectoryOffset: 139,
		TileDataOffset:      139,
		TileDataLength:      70,
		InternalCompression: CompressionNone,
		TileCompression:     CompressionNone,
		TileType:            TileTypePNG,
		MinZoom:             1,
		MaxZoom:             5,
		MinLonE7:            -1800000000,
		MinLatE7:            -850000000,
		MaxLonE7:            1800000000,
		MaxLatE7:            850000000,
		CenterZoom:          2,
		CenterLonE7:         15000000,
		CenterLatE7:         -25000000,
	}
}

type memSource struct {
	data  []byte
	reads atomic.Int32
}

func (m *memSource) ReadRange(_ context.Context, offset, length uint64) ([]byte, error) {
	m.reads.Add(1)
	if offset >= uint64(len(m.data)) {
		return []byte{}, nil
	}
	end := min(offset+length, uint64(len(m.data)))
	return m.data[offset:end], nil
}

func (m *memSource) Close() error { return nil }

type errSource struct {
	err error
}

func (s *errSource) ReadRange(context.Context, uint64, uint64) ([]byte, error) { return nil, s.err }
func (s *errSource) Close() error                                              { return nil }

// Simulates a remote failure once reads reach tile data, keeping header/root/metadata reads working.
type failAfterHeaderSource struct {
	data     []byte
	failFrom uint64
}

func (s *failAfterHeaderSource) ReadRange(_ context.Context, offset, length uint64) ([]byte, error) {
	if offset >= s.failFrom {
		return nil, pkg.RemoteServerError{StatusCode: 503}
	}
	end := min(offset+length, uint64(len(s.data)))
	return s.data[offset:end], nil
}

func (s *failAfterHeaderSource) Close() error { return nil }

type testTile struct {
	z    uint8
	x, y uint32
	data []byte
}

type archiveOptions struct {
	tiles               []testTile
	tileType            TileType
	internalCompression Compression
	tileCompression     Compression
	metadata            string
	leafSize            int // entries per leaf directory; 0 puts every entry in the root
}

func compressFor(t *testing.T, c Compression, b []byte) []byte {
	if c == CompressionGzip {
		return gzipBytes(t, b)
	}
	return b
}

func buildArchive(t *testing.T, opts archiveOptions) []byte {
	t.Helper()

	tiles := append([]testTile(nil), opts.tiles...)
	sort.Slice(tiles, func(i, j int) bool {
		return ZxyToID(tiles[i].z, tiles[i].x, tiles[i].y) < ZxyToID(tiles[j].z, tiles[j].x, tiles[j].y)
	})

	var tileData []byte
	entries := make([]entry, 0, len(tiles))
	minZoom, maxZoom := uint8(255), uint8(0)
	for _, tile := range tiles {
		b := compressFor(t, opts.tileCompression, tile.data)
		entries = append(entries, entry{TileID: ZxyToID(tile.z, tile.x, tile.y), Offset: uint64(len(tileData)), Length: uint32(len(b)), RunLength: 1})
		tileData = append(tileData, b...)
		minZoom = min(minZoom, tile.z)
		maxZoom = max(maxZoom, tile.z)
	}

	var leaves []byte
	rootEntries := entries
	if opts.leafSize > 0 {
		rootEntries = nil
		for start := 0; start < len(entries); start += opts.leafSize {
			chunk := entries[start:min(start+opts.leafSize, len(entries))]
			b := compressFor(t, opts.internalCompression, serializeEntries(chunk))
			rootEntries = append(rootEntries, entry{TileID: chunk[0].TileID, Offset: uint64(len(leaves)), Length: uint32(len(b)), RunLength: 0})
			leaves = append(leaves, b...)
		}
	}

	root := compressFor(t, opts.internalCompression, serializeEntries(rootEntries))
	metadata := compressFor(t, opts.internalCompression, []byte(opts.metadata))

	h := testHeader()
	h.InternalCompression = opts.internalCompression
	h.TileCompression = opts.tileCompression
	h.TileType = opts.tileType
	h.MinZoom, h.MaxZoom = minZoom, maxZoom
	h.RootOffset = HeaderLength
	h.RootLength = uint64(len(root))
	h.MetadataOffset = h.RootOffset + h.RootLength
	h.MetadataLength = uint64(len(metadata))
	h.LeafDirectoryOffset = h.MetadataOffset + h.MetadataLength
	h.LeafDirectoryLength = uint64(len(leaves))
	h.TileDataOffset = h.LeafDirectoryOffset + h.LeafDirectoryLength
	h.TileDataLength = uint64(len(tileData))

	out := serializeHeader(t, h)
	out = append(out, root...)
	out = append(out, metadata...)
	out = append(out, leaves...)
	out = append(out, tileData...)

	return out
}

func tilesAtZoom2() []testTile {
	tiles := make([]testTile, 0, 8)
	for x := range uint32(4) {
		for y := range uint32(2) {
			tiles = append(tiles, testTile{z: 2, x: x, y: y, data: []byte{byte(x), byte(y), 'm', 'v', 't'}})
		}
	}
	return tiles
}
