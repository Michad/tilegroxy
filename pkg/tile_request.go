// Copyright 2024 Michael Davis
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
package pkg

import (
	"fmt"
	"math"
	"strconv"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/mmcloughlin/geohash"
)

const (
	SRIDWGS84            = 4326
	SRIDPsuedoMercator   = 3857
	MaxZoom              = 21
	delta                = .00000001
	maxLat               = 85.0511
	minLat               = -85.0511
	maxLong              = 180
	minLong              = -180
	max3857CoordInMeters = 20037508.342789
)

func convertLat4326To3857(lat float64) float64 {
	return math.Log(math.Tan((90+lat)*math.Pi/360)) / (math.Pi / maxLong) * (max3857CoordInMeters / maxLong)
}
func convertLon4326To3857(lon float64) float64 {
	return lon * max3857CoordInMeters / maxLong
}

type TileRequest struct {
	LayerName string
	Z         int
	X         int
	Y         int
}

func (t TileRequest) GetBounds() (*Bounds, error) {
	return t.GetBoundsProjection(SRIDWGS84)
}

func (t TileRequest) GetBoundsProjection(srid uint) (*Bounds, error) {
	if t.Z < 0 || t.Z > MaxZoom {
		return nil, RangeError{ParamName: "Z", MinValue: 0, MaxValue: MaxZoom}
	}
	if srid != SRIDWGS84 && srid != SRIDPsuedoMercator {
		return nil, InvalidSridError{srid}
	}

	z := float64(t.Z)
	x := float64(t.X)
	y := float64(t.Y)

	n := math.Exp2(z)

	if x < 0 || x >= n {
		return nil, RangeError{"X", 0, n - 1}
	}

	if y < 0 || y >= n {
		return nil, RangeError{"Y", 0, n - 1}
	}

	x1 := x/n*360 - maxLong
	x2 := (x+1)/n*360 - maxLong
	y1 := maxLong / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*y/n)))
	y2 := maxLong / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*(y+1)/n)))

	north := math.Max(y1, y2)
	south := math.Min(y1, y2)
	west := math.Min(x2, x1)
	east := math.Max(x2, x1)

	if srid == SRIDWGS84 {
		return &Bounds{south, north, west, east, srid}, nil
	}

	return &Bounds{
		convertLat4326To3857(south),
		convertLat4326To3857(north),
		convertLon4326To3857(west),
		convertLon4326To3857(east),
		srid}, nil
}

func (t TileRequest) IntersectsBounds(b Bounds) (bool, error) {
	return t.IntersectsBoundsProjection(b, SRIDWGS84)
}

func (t TileRequest) IntersectsBoundsProjection(b Bounds, srid uint) (bool, error) {
	// Null island bounds mean everything
	if b.IsNullIsland() {
		return true, nil
	}

	b2, err := t.GetBoundsProjection(srid)
	if err != nil {
		return false, err
	}

	return b2.Intersects(b), nil
}

// Slash separated
func (t TileRequest) String() string {
	return t.StringWithSeparator("/")
}

func (t TileRequest) StringWithSeparator(sep string) string {
	return t.LayerName + sep + strconv.Itoa(t.Z) + sep + strconv.Itoa(t.X) + sep + strconv.Itoa(t.Y)
}

type Bounds struct {
	South float64
	North float64
	West  float64
	East  float64
	SRID  uint
}

func BoundsFromConfig(b config.BoundsConfig) Bounds {
	return Bounds{South: b.South, North: b.North, West: b.West, East: b.East, SRID: SRIDWGS84}
}

func (b Bounds) ToConfig() config.BoundsConfig {
	return config.BoundsConfig{South: b.South, North: b.North, West: b.West, East: b.East}
}

// atan(sinh(pi)) at full precision. maxLat/minLat are truncated, which suits grid clamping but not reported extents
const preciseMaxLat = 85.0511287798066

// The whole world clipped to the Web Mercator grid's latitude range. The default extent for anything unbounded
func WorldBounds() Bounds {
	return Bounds{South: -preciseMaxLat, North: preciseMaxLat, West: -maxLong, East: maxLong, SRID: SRIDWGS84}
}

func NewBoundsFromGeohash(hashStr string) (Bounds, error) {
	err := geohash.Validate(hashStr)
	if err != nil {
		return Bounds{}, err
	}

	bbox := geohash.BoundingBox(hashStr)

	return Bounds{South: bbox.MinLat, North: bbox.MaxLat, West: bbox.MinLng, East: bbox.MaxLng, SRID: SRIDWGS84}, nil
}

// All coordinates are zero
func (b Bounds) IsNullIsland() bool {
	return b.East == 0 && b.North == 0 && b.West == 0 && b.South == 0
}

// Any part of the two bounds touch
func (b Bounds) Intersects(b2 Bounds) bool {
	return b2.North > b.South && b2.South < b.North && b2.East > b.West && b2.West < b.East
}

// b2 is fully contained by b
func (b Bounds) Contains(b2 Bounds) bool {
	return b2.South+delta >= b.South && b2.North <= b.North+delta && b2.West+delta >= b.West && b2.East <= b.East+delta
}

// Only meaningful when the two intersect. Otherwise South > North or West > East
func (b Bounds) IntersectionWith(b2 Bounds) Bounds {
	return Bounds{
		South: math.Max(b.South, b2.South),
		North: math.Min(b.North, b2.North),
		West:  math.Max(b.West, b2.West),
		East:  math.Min(b.East, b2.East),
		SRID:  b.SRID,
	}
}

func (b Bounds) ContainsPoint(x float64, y float64) bool {
	return y+delta >= b.South && y <= b.North+delta && x+delta >= b.West && x <= b.East+delta
}

func (b Bounds) Width() float64 {
	return math.Abs(b.East - b.West)
}

func (b Bounds) Height() float64 {
	return math.Abs(b.North - b.South)
}

func (b Bounds) Centroid() (float64, float64) {
	return (b.West + b.East) / 2.0, (b.North + b.South) / 2.0
}

func (b Bounds) BufferRelative(pct float64) Bounds {
	deltaW := b.Width() * pct / 2
	deltaH := b.Height() * pct / 2

	return Bounds{
		North: b.North + deltaH,
		South: b.South - deltaH,
		West:  b.West - deltaW,
		East:  b.East + deltaW,
		SRID:  b.SRID,
	}
}

func (b Bounds) ConvertToPsuedoMercatorRange() Bounds {
	if b.SRID != SRIDPsuedoMercator {
		return Bounds{
			convertLat4326To3857(b.South),
			convertLat4326To3857(b.North),
			convertLon4326To3857(b.West),
			convertLon4326To3857(b.East),
			SRIDPsuedoMercator}
	}

	return b
}

func (b Bounds) ConfineToPsuedoMercatorRange() Bounds {
	if b.SRID == SRIDPsuedoMercator {
		return Bounds{
			SRID:  b.SRID,
			North: math.Max(math.Min(b.North, max3857CoordInMeters), -max3857CoordInMeters),
			South: math.Max(math.Min(b.South, max3857CoordInMeters), -max3857CoordInMeters),
			West:  math.Max(math.Min(b.West, max3857CoordInMeters), -max3857CoordInMeters),
			East:  math.Max(math.Min(b.East, max3857CoordInMeters), -max3857CoordInMeters),
		}
	}

	return Bounds{
		SRID:  b.SRID,
		North: math.Max(math.Min(b.North, maxLat), minLat),
		South: math.Max(math.Min(b.South, maxLat), minLat),
		West:  math.Max(math.Min(b.West, maxLong), minLong),
		East:  math.Max(math.Min(b.East, maxLong), minLong),
	}
}

func (b Bounds) ToWKT() string {
	return fmt.Sprintf("POLYGON((%.7f %.7f,%.7f %.7f,%.7f %.7f,%.7f %.7f,%.7f %.7f))", b.West, b.South, b.West, b.North, b.East, b.North, b.East, b.South, b.West, b.South)
}

func (b Bounds) ToEWKT() string {
	return fmt.Sprintf("SRID=%v;%v", b.SRID, b.ToWKT())
}
