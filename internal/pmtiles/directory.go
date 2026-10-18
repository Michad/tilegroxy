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
	"math"
	"sort"
)

type entry struct {
	TileID    uint64
	Offset    uint64
	Length    uint32
	RunLength uint32
}

var errInvalidDirectory = errors.New("pmtiles: invalid directory")

func deserializeEntries(data []byte) ([]entry, error) {
	r := bytes.NewReader(data)

	count, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidDirectory, err)
	}
	// Every entry needs at least four bytes, so a larger count means corruption
	if count > uint64(len(data))/4 {
		return nil, fmt.Errorf("%w: %d entries in %d bytes", errInvalidDirectory, count, len(data))
	}

	entries := make([]entry, count)

	var last uint64
	for i := range entries {
		delta, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errInvalidDirectory, err)
		}
		last += delta
		entries[i].TileID = last
	}

	for i := range entries {
		v, err := readUint32(r)
		if err != nil {
			return nil, err
		}
		entries[i].RunLength = v
	}

	for i := range entries {
		v, err := readUint32(r)
		if err != nil {
			return nil, err
		}
		entries[i].Length = v
	}

	for i := range entries {
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errInvalidDirectory, err)
		}

		switch {
		case v == 0 && i > 0:
			entries[i].Offset = entries[i-1].Offset + uint64(entries[i-1].Length)
		case v == 0:
			return nil, fmt.Errorf("%w: first entry has no offset", errInvalidDirectory)
		default:
			entries[i].Offset = v - 1
		}
	}

	return entries, nil
}

func readUint32(r *bytes.Reader) (uint32, error) {
	v, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidDirectory, err)
	}
	if v > math.MaxUint32 {
		return 0, fmt.Errorf("%w: value %d overflows", errInvalidDirectory, v)
	}

	return uint32(v), nil // #nosec G115 -- range checked above
}

// A zero run length marks a leaf directory covering every ID from its TileID to the next entry
func findTile(entries []entry, tileID uint64) (entry, bool) {
	i := sort.Search(len(entries), func(k int) bool { return entries[k].TileID > tileID })
	if i == 0 {
		return entry{}, false
	}

	e := entries[i-1]
	if e.TileID == tileID || e.RunLength == 0 || tileID-e.TileID < uint64(e.RunLength) {
		return e, true
	}

	return entry{}, false
}
