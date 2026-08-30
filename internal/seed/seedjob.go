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

type SeedJob struct {
	layerName string
	bounds    pkg.Bounds
	ranges    []pkg.SingleZoomRange
	count     uint64
}

func NewSeedJob(layerName string, bounds pkg.Bounds, zooms []uint) (*SeedJob, error) {
	sorted := slices.Clone(zooms)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	e := &SeedJob{layerName: layerName, bounds: bounds}

	for _, z := range sorted {
		r, err := bounds.ConstructSingleZoomRange(z)
		if err != nil {
			return nil, err
		}

		e.ranges = append(e.ranges, r)
		e.count += r.Count()
	}

	return e, nil
}

// Count is the total number of tiles the run covers
func (e *SeedJob) Count() uint64 {
	return e.count
}

func (e *SeedJob) Bounds() pkg.Bounds {
	return e.bounds
}

// In enumeration order.
func (e *SeedJob) Zooms() []uint {
	zooms := make([]uint, 0, len(e.ranges))
	for _, r := range e.ranges {
		zooms = append(zooms, r.Z)
	}

	return zooms
}

// yields every tile in the run in a fixed order: ascending zoom, then ascending x, then
// ascending y.
func (e *SeedJob) All() iter.Seq2[uint64, pkg.TileRequest] {
	return e.From(0)
}

// yields the tiles of the run starting at the given position in the sequence, skipping
// everything before it.
func (e *SeedJob) From(start uint64) iter.Seq2[uint64, pkg.TileRequest] {
	return func(yield func(uint64, pkg.TileRequest) bool) {
		var i uint64

		for _, r := range e.ranges {
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
