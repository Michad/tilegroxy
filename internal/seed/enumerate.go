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

package seed

import (
	"iter"
	"slices"

	"github.com/Michad/tilegroxy/pkg"
)

// Enumeration describes the full set of tiles a seed run covers. It's a description rather than a
// list: the tiles are produced on demand so a run covering millions of tiles doesn't need them all
// in memory at once.
type Enumeration struct {
	layerName string
	bounds    pkg.Bounds
	ranges    []pkg.TileRange
	count     uint64
}

// NewEnumeration works out the tile grid each zoom level covers. Zoom levels are sorted and
// deduplicated so the same arguments always produce the same sequence regardless of the order the
// zoom flags were given in.
func NewEnumeration(layerName string, bounds pkg.Bounds, zooms []uint) (*Enumeration, error) {
	sorted := slices.Clone(zooms)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	e := &Enumeration{layerName: layerName, bounds: bounds}

	for _, z := range sorted {
		r, err := bounds.FindTileRange(z)
		if err != nil {
			return nil, err
		}

		e.ranges = append(e.ranges, r)
		e.count += r.Count()
	}

	return e, nil
}

// Count is the total number of tiles the run covers, calculated from the bounds rather than by
// enumerating them.
func (e *Enumeration) Count() uint64 {
	return e.count
}

// Bounds is the area the run covers.
func (e *Enumeration) Bounds() pkg.Bounds {
	return e.bounds
}

// Zooms lists the zoom levels the run covers, in enumeration order.
func (e *Enumeration) Zooms() []uint {
	zooms := make([]uint, 0, len(e.ranges))
	for _, r := range e.ranges {
		zooms = append(zooms, r.Z)
	}

	return zooms
}

// All yields every tile in the run in a fixed order: ascending zoom, then ascending x, then
// ascending y. The order is what makes resuming meaningful, since a position in the sequence refers
// to the same tile on every run with the same arguments.
func (e *Enumeration) All() iter.Seq2[uint64, pkg.TileRequest] {
	return e.From(0)
}

// From yields the tiles of the run starting at the given position in the sequence, skipping
// everything before it. The yielded index is the position in the full sequence, so the first tile
// yielded by From(n) has index n.
func (e *Enumeration) From(start uint64) iter.Seq2[uint64, pkg.TileRequest] {
	return func(yield func(uint64, pkg.TileRequest) bool) {
		var i uint64

		for _, r := range e.ranges {
			// Whole zoom levels below the resume point are skipped without touching each tile.
			if size := r.Count(); i+size <= start {
				i += size
				continue
			}

			for x := r.XMin; x < r.XMax; x++ {
				for y := r.YMin; y < r.YMax; y++ {
					if i >= start {
						if !yield(i, pkg.TileRequest{LayerName: e.layerName, Z: int(r.Z), X: x, Y: y}) { // #nosec G115 -- zoom is validated against MaxZoom
							return
						}
					}
					i++
				}
			}
		}
	}
}
