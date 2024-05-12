package pkg

import (
	"fmt"
	"math"
)

type TileRequest struct {
	LayerName string
	Z         int
	X         int
	Y         int
}

type Bounds struct {
	MinLat  float64
	MaxLat  float64
	MinLong float64
	MaxLong float64
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
	return fmt.Sprintf("Param %v must be between %v and %v", e.ParamName, e.MinValue, e.MaxValue)
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

	minLong := x/n*360 - 180
	maxLat := 180 / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*y/n)))
	maxLong := (x+1)/n*360 - 180
	minLat := 180 / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*(y-1)/n)))

	return &Bounds{minLat, maxLat, minLong, maxLong}, nil
}
