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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/maypok86/otter"
)

const (
	// The spec guarantees the header and root directory fit in the first 16 KiB.
	initialFetchLength = 16384
	maxDirectoryDepth  = 4
	leafCacheSize      = 256
)

const maxSupportedZoom = 31

var (
	errTruncated       = errors.New("pmtiles: archive is truncated")
	errTooDeep         = errors.New("pmtiles: directory nesting exceeds the spec limit")
	errInvalidOffset   = errors.New("pmtiles: invalid archive: entry offset overflows")
	errSectionTooLarge = errors.New("pmtiles: section exceeds the configured byte limit")
	errZoomUnsupported = fmt.Errorf("pmtiles: max zoom above %d is not supported, Hilbert IDs only cover 0-%d", maxSupportedZoom, maxSupportedZoom)
)

type Archive struct {
	source    Source
	header    Header
	root      []entry
	metadata  map[string]any
	leaves    otter.Cache[uint64, []entry]
	maxLength uint64
}

func Open(ctx context.Context, source Source, maxLength int) (*Archive, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("pmtiles: maximum length must be positive, got %d", maxLength)
	}

	head, err := source.ReadRange(ctx, 0, initialFetchLength)
	if err != nil {
		return nil, fmt.Errorf("pmtiles: reading header: %w", err)
	}

	header, err := parseHeader(head)
	if err != nil {
		return nil, err
	}

	if !header.InternalCompression.supported() || !header.TileCompression.supported() {
		return nil, errUnsupportedCompression
	}
	if header.MaxZoom > maxSupportedZoom {
		return nil, errZoomUnsupported
	}

	a := &Archive{source: source, header: header, maxLength: uint64(maxLength)} // #nosec G115 -- guarded positive above

	rootBytes, err := a.section(ctx, head, header.RootOffset, header.RootLength)
	if err != nil {
		return nil, err
	}
	if a.root, err = a.directory(rootBytes); err != nil {
		return nil, err
	}

	a.metadata = map[string]any{}
	if header.MetadataLength > 0 {
		raw, err := a.section(ctx, head, header.MetadataOffset, header.MetadataLength)
		if err != nil {
			return nil, err
		}
		raw, err = decompress(raw, header.InternalCompression, a.maxLength)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &a.metadata); err != nil {
			return nil, fmt.Errorf("pmtiles: invalid metadata: %w", err)
		}
		// A literal `null` metadata document unmarshals to a nil map, breaking the never-nil contract.
		if a.metadata == nil {
			a.metadata = map[string]any{}
		}
	}

	leaves, err := otter.MustBuilder[uint64, []entry](leafCacheSize).Build()
	if err != nil {
		return nil, err
	}
	a.leaves = leaves

	return a, nil
}

func (a *Archive) Header() Header {
	return a.header
}

func (a *Archive) Metadata() map[string]any {
	return a.metadata
}

func (a *Archive) Close() error {
	a.leaves.Close()
	return a.source.Close()
}

func (a *Archive) Tile(ctx context.Context, z uint8, x, y uint32) ([]byte, bool, error) {
	if z < a.header.MinZoom || z > a.header.MaxZoom {
		return nil, false, nil
	}
	// ZxyToID ignores bits beyond 1<<z, so out-of-range x/y would otherwise alias to a valid tile.
	if z < 32 && (x >= uint32(1)<<z || y >= uint32(1)<<z) {
		return nil, false, nil
	}

	id := ZxyToID(z, x, y)
	dir := a.root

	for range maxDirectoryDepth {
		e, ok := findTile(dir, id)
		if !ok {
			return nil, false, nil
		}

		if e.RunLength > 0 {
			offset := a.header.TileDataOffset + e.Offset
			if offset < e.Offset {
				return nil, false, errInvalidOffset
			}

			data, err := a.read(ctx, offset, uint64(e.Length))
			if err != nil {
				return nil, false, err
			}
			data, err = decompress(data, a.header.TileCompression, a.maxLength)
			return data, err == nil, err
		}

		var err error
		if dir, err = a.leaf(ctx, e); err != nil {
			return nil, false, err
		}
	}

	return nil, false, errTooDeep
}

func (a *Archive) leaf(ctx context.Context, e entry) ([]entry, error) {
	if cached, ok := a.leaves.Get(e.Offset); ok {
		return cached, nil
	}

	offset := a.header.LeafDirectoryOffset + e.Offset
	if offset < e.Offset {
		return nil, errInvalidOffset
	}

	raw, err := a.read(ctx, offset, uint64(e.Length))
	if err != nil {
		return nil, err
	}

	entries, err := a.directory(raw)
	if err != nil {
		return nil, err
	}

	a.leaves.Set(e.Offset, entries)

	return entries, nil
}

func (a *Archive) directory(raw []byte) ([]entry, error) {
	data, err := decompress(raw, a.header.InternalCompression, a.maxLength)
	if err != nil {
		return nil, err
	}

	return deserializeEntries(data)
}

// Reuses the initial fetch when possible so small archives open with a single read.
func (a *Archive) section(ctx context.Context, head []byte, offset, length uint64) ([]byte, error) {
	if offset <= uint64(len(head)) && length <= uint64(len(head))-offset {
		return head[offset : offset+length], nil
	}

	return a.read(ctx, offset, length)
}

func (a *Archive) read(ctx context.Context, offset, length uint64) ([]byte, error) {
	if length > a.maxLength {
		return nil, fmt.Errorf("%w: %d bytes exceeds the %d byte limit", errSectionTooLarge, length, a.maxLength)
	}

	data, err := a.source.ReadRange(ctx, offset, length)
	if err != nil {
		return nil, fmt.Errorf("pmtiles: reading %d bytes at offset %d: %w", length, offset, err)
	}
	if uint64(len(data)) != length {
		return nil, errTruncated
	}

	return data, nil
}
