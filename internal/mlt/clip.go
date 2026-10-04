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

package mlt

import (
	"math"
	"slices"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/clip"
)

// A closed ring needs three distinct vertices plus the closing one
const minRingPoints = 4

// Trims every geometry to a bound in tile coordinates, dropping features left empty
func (l Layer) Clip(b orb.Bound) Layer {
	geoms := make([]orb.Geometry, 0, len(l.Geometries))
	keep := make([]int, 0, len(l.Geometries))

	for i, g := range l.Geometries {
		if clipped := snap(clip.Geometry(b, g)); clipped != nil {
			geoms = append(geoms, clipped)
			keep = append(keep, i)
		}
	}

	out := l.Select(keep)
	out.Geometries = geoms

	return out
}

// Clipping adds fractional intersections, so vertices are rounded back to the grid and collapsed parts removed
func snap(g orb.Geometry) orb.Geometry {
	switch v := g.(type) {
	case orb.Point:
		return roundPoint(v)
	case orb.MultiPoint:
		out := make(orb.MultiPoint, len(v))
		for i, p := range v {
			out[i] = roundPoint(p)
		}

		if len(out) > 0 {
			return out
		}
	case orb.LineString:
		if line := snapLine(v); line != nil {
			return line
		}
	case orb.MultiLineString:
		var out orb.MultiLineString
		for _, line := range v {
			if snapped := snapLine(line); snapped != nil {
				out = append(out, snapped)
			}
		}

		if len(out) > 0 {
			return out
		}
	case orb.Polygon:
		if p := snapPolygon(v); p != nil {
			return p
		}
	case orb.MultiPolygon:
		var out orb.MultiPolygon
		for _, p := range v {
			if snapped := snapPolygon(p); snapped != nil {
				out = append(out, snapped)
			}
		}

		if len(out) > 0 {
			return out
		}
	}

	return nil
}

func roundPoint(p orb.Point) orb.Point {
	return orb.Point{math.Round(p[0]), math.Round(p[1])}
}

func snapPoints(points []orb.Point) []orb.Point {
	out := make([]orb.Point, len(points))
	for i, p := range points {
		out[i] = roundPoint(p)
	}

	return slices.Compact(out)
}

func snapLine(l orb.LineString) orb.LineString {
	out := snapPoints(l)
	if len(out) < 2 {
		return nil
	}

	return out
}

// A collapsed outer ring drops the polygon, while a collapsed hole just disappears
func snapPolygon(p orb.Polygon) orb.Polygon {
	var out orb.Polygon

	for i, ring := range p {
		snapped := orb.Ring(snapPoints(ring))
		if len(snapped) >= minRingPoints {
			out = append(out, snapped)
		} else if i == 0 {
			return nil
		}
	}

	return out
}
