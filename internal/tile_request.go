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
package internal

import (
	"fmt"
	"math"
	"strconv"

	"github.com/mmcloughlin/geohash"
)

type TileRequest struct {
	LayerName string
	Z         int
	X         int
	Y         int
}

type Bounds struct {
	South float64
	North float64
	West  float64
	East  float64
}

const (
	MaxZoom = 21
)

type RangeError struct {
	ParamName string
	MinValue  float64
	MaxValue  float64
}

func (e RangeError) Error() string {
	// notest
	return fmt.Sprintf("Param %v must be between %v and %v", e.ParamName, e.MinValue, e.MaxValue)
}

type TooManyTilesError struct {
	NumTiles uint64
}

func (e TooManyTilesError) Error() string {
	// notest
	return fmt.Sprintf("too many tiles to return (%v > 10000)", e.NumTiles)
}

func (t TileRequest) GetBounds() (*Bounds, error) {
	if t.Z < 0 || t.Z > MaxZoom {
		return nil, RangeError{ParamName: "Z", MinValue: 0, MaxValue: MaxZoom}
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

	x1 := x/n*360 - 180
	x2 := (x+1)/n*360 - 180
	y1 := 180 / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*y/n)))
	y2 := 180 / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*(y+1)/n)))

	north := math.Max(y1, y2)
	south := math.Min(y1, y2)
	west := math.Min(x2, x1)
	east := math.Max(x2, x1)

	return &Bounds{south, north, west, east}, nil
}

func (t TileRequest) IntersectsBounds(b Bounds) (bool, error) {
	//Treat null-island only bounds as everything
	if b.North == 0 && b.East == 0 && b.South == 0 && b.West == 0 {
		return true, nil
	}

	b2, err := t.GetBounds()
	if err != nil {
		return false, err
	}

	return b2.Intersects(b), nil
}

func (t TileRequest) String() string {
	return t.LayerName + "/" + strconv.Itoa(t.Z) + "/" + strconv.Itoa(t.X) + "/" + strconv.Itoa(t.Y)
}

func (b Bounds) FindTiles(layerName string, zoom uint, force bool) (*[]TileRequest, error) {
	z := float64(zoom)

	lonMin := b.West
	for lonMin > 180 {
		lonMin -= 180
	}
	for lonMin < -180 {
		lonMin += 180
	}
	lonMax := b.East
	for lonMax > 180 {
		lonMax -= 180
	}
	for lonMax < -180 {
		lonMax += 180
	}

	n := math.Exp2(z)
	latMin := math.Min(85.0511, math.Max(-85.0511, b.South)) * math.Pi / 180.0
	latMax := math.Min(85.0511, math.Max(-85.0511, b.North)) * math.Pi / 180.0

	x1 := n * ((lonMin + 180) / 360)
	x2 := n * ((lonMax + 180) / 360)
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

	numTiles := uint64(xMax-xMin) * uint64(yMax-yMin)

	if numTiles > 10000 && !force {
		return nil, TooManyTilesError{NumTiles: numTiles}
	}

	if numTiles > math.MaxInt32 {
		return nil, TooManyTilesError{NumTiles: numTiles}
	}

	result := make([]TileRequest, int32(numTiles))

	for x := xMin; x < xMax; x++ {
		for y := yMin; y < yMax; y++ {
			result[(y-yMin)*(xMax-xMin)+x-xMin] = TileRequest{layerName, int(zoom), x, y}
		}
	}

	return &result, nil
}

// This bounds just has the default values (all coords are 0)
func (b Bounds) IsNullIsland() bool {
	return b.East == 0 && b.North == 0 && b.West == 0 && b.South == 0
}

// Any part of this bounds and the bounds passed in touch
func (b Bounds) Intersects(b2 Bounds) bool {
	return b2.North > b.South && b2.South < b.North && b2.East > b.West && b2.West < b.East
}

// The bounds passed in is fully contained by this bounds
func (b Bounds) Contains(b2 Bounds) bool {
	return b2.South >= b.South && b2.North <= b.North && b2.West >= b.West && b2.East <= b.East
}

func NewBoundsFromGeohash(hashStr string) (Bounds, error) {
	err := geohash.Validate(hashStr)
	if err != nil {
		return Bounds{}, err
	}

	bbox := geohash.BoundingBox(hashStr)

	return Bounds{South: bbox.MinLat, North: bbox.MaxLat, West: bbox.MinLng, East: bbox.MaxLng}, nil
}
