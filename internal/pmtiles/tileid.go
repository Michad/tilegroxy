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

const (
	// Zooms below z hold 4^0 + ... + 4^(z-1) = (4^z - 1) / lowerZoomDivisor tiles
	lowerZoomDivisor = 3
	quadrantFactor   = 3
)

// Hilbert curve tile ID used by PMTiles v3
func ZxyToID(z uint8, x, y uint32) uint64 {
	acc := (uint64(1)<<(2*uint64(z)) - 1) / lowerZoomDivisor

	for a := z; a > 0; a-- {
		shift := a - 1
		s := uint32(1) << shift
		rx := s & x
		ry := s & y
		acc += uint64((quadrantFactor*rx)^ry) << shift
		x, y = rotate(s, x, y, rx, ry)
	}

	return acc
}

func rotate(n, x, y, rx, ry uint32) (uint32, uint32) {
	if ry == 0 {
		if rx != 0 {
			x = n - 1 - x
			y = n - 1 - y
		}
		return y, x
	}

	return x, y
}
