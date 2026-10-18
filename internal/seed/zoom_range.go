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
	"math"

	"github.com/Michad/tilegroxy/pkg"
)

const (
	maxLat  = 85.0511
	minLat  = -85.0511
	maxLong = 180
	minLong = -180
)

// Tiles within a single zoom level
type SingleZoomRange struct {
	Z                      uint
	XMin, XMax, YMin, YMax int
}

func (r SingleZoomRange) Count() uint64 {
	return uint64(r.XMax-r.XMin) * uint64(r.YMax-r.YMin) // #nosec G115 -- int->uint64 can't overflow until 128 bit processors come out
}

func NewSingleZoomRange(b pkg.Bounds, zoom uint) (SingleZoomRange, error) {
	if zoom > pkg.MaxZoom {
		return SingleZoomRange{}, pkg.RangeError{ParamName: "z", MinValue: 0, MaxValue: pkg.MaxZoom}
	}

	// Bounds crossing the antimeridian would invert the x range, underflowing Count
	if b.West > b.East {
		return SingleZoomRange{}, pkg.RangeError{ParamName: "east", MinValue: b.West, MaxValue: maxLong}
	}

	// An inverted y range either underflows Count or silently seeds a different latitude band
	if b.South > b.North {
		return SingleZoomRange{}, pkg.RangeError{ParamName: "north", MinValue: b.South, MaxValue: maxLat}
	}

	z := float64(zoom)

	lonMin := b.West
	for lonMin > maxLong {
		lonMin -= (maxLong - minLong)
	}
	for lonMin < minLong {
		lonMin -= (minLong - maxLong)
	}
	lonMax := b.East
	for lonMax > maxLong {
		lonMax -= (maxLong - minLong)
	}
	for lonMax < minLong {
		lonMax -= (minLong - maxLong)
	}

	// Normalizing each edge independently loses the crossing, inverting the x range
	if lonMin > lonMax {
		return SingleZoomRange{}, pkg.RangeError{ParamName: "east", MinValue: lonMin, MaxValue: maxLong}
	}

	n := math.Exp2(z)
	latMin := math.Min(maxLat, math.Max(minLat, b.South)) * math.Pi / maxLong
	latMax := math.Min(maxLat, math.Max(minLat, b.North)) * math.Pi / maxLong

	x1 := n * ((lonMin + maxLong) / 360)
	x2 := n * ((lonMax + maxLong) / 360)
	y1 := math.Ceil(n * (1 - (math.Log(math.Tan(latMin)+1.0/math.Cos(latMin)) / math.Pi)) / 2)
	y2 := math.Floor(n * (1 - (math.Log(math.Tan(latMax)+1.0/math.Cos(latMax)) / math.Pi)) / 2)

	yMin := int(math.Min(n, math.Max(0, y2)))
	yMax := int(math.Min(n, math.Max(0, y1)))
	xMin := int(math.Min(n, math.Max(0, x1)))
	xMax := int(math.Min(n, math.Max(0, x2)))

	if xMin == xMax {
		xMax = xMin + 1
	}
	if yMin == yMax {
		yMax = yMin + 1
	}

	return SingleZoomRange{Z: zoom, XMin: xMin, XMax: xMax, YMin: yMin, YMax: yMax}, nil
}
