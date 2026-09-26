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
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
)

const (
	HeaderLength = 127
	specVersion  = 3
	e7Scale      = 1e7
)

type TileType uint8

const (
	TileTypeUnknown TileType = 0
	TileTypeMVT     TileType = 1
	TileTypePNG     TileType = 2
	TileTypeJPEG    TileType = 3
	TileTypeWebP    TileType = 4
	TileTypeAVIF    TileType = 5
	TileTypeMLT     TileType = 6
)

var contentTypes = map[TileType]string{
	TileTypeMVT:  "application/vnd.mapbox-vector-tile",
	TileTypePNG:  "image/png",
	TileTypeJPEG: "image/jpeg",
	TileTypeWebP: "image/webp",
	TileTypeAVIF: "image/avif",
}

func (t TileType) ContentType() (string, bool) {
	ct, ok := contentTypes[t]
	return ct, ok
}

var headerMagic = [7]byte{'P', 'M', 'T', 'i', 'l', 'e', 's'}

// Field order and sizes mirror the v3 spec's byte layout so binary.Read can decode it directly.
type Header struct {
	Magic               [7]byte
	Version             uint8
	RootOffset          uint64
	RootLength          uint64
	MetadataOffset      uint64
	MetadataLength      uint64
	LeafDirectoryOffset uint64
	LeafDirectoryLength uint64
	TileDataOffset      uint64
	TileDataLength      uint64
	AddressedTilesCount uint64
	TileEntriesCount    uint64
	TileContentsCount   uint64
	Clustered           uint8
	InternalCompression Compression
	TileCompression     Compression
	TileType            TileType
	MinZoom             uint8
	MaxZoom             uint8
	MinLonE7            int32
	MinLatE7            int32
	MaxLonE7            int32
	MaxLatE7            int32
	CenterZoom          uint8
	CenterLonE7         int32
	CenterLatE7         int32
}

var errNotPMTiles = errors.New("pmtiles: not a PMTiles archive")

func parseHeader(d []byte) (Header, error) {
	var h Header

	if len(d) < HeaderLength {
		return h, fmt.Errorf("%w: header is %d bytes, expected %d", errNotPMTiles, len(d), HeaderLength)
	}

	if err := binary.Read(bytes.NewReader(d[:HeaderLength]), binary.LittleEndian, &h); err != nil {
		return h, err
	}

	if h.Magic != headerMagic {
		return h, errNotPMTiles
	}

	if h.Version != specVersion {
		return h, fmt.Errorf("pmtiles: spec version %d is not supported, only version %d", h.Version, specVersion)
	}

	return h, nil
}

func (h Header) Bounds() pkg.Bounds {
	return pkg.Bounds{
		South: e7(h.MinLatE7),
		North: e7(h.MaxLatE7),
		West:  e7(h.MinLonE7),
		East:  e7(h.MaxLonE7),
		SRID:  pkg.SRIDWGS84,
	}
}

func (h Header) Center() []float64 {
	return []float64{e7(h.CenterLonE7), e7(h.CenterLatE7), float64(h.CenterZoom)}
}

func e7(v int32) float64 {
	return float64(v) / e7Scale
}
