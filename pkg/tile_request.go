package pkg

import (
	"fmt"
	"math"
	"strconv"
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

func (b Bounds) FindTiles(layerName string, zoom uint, force bool) (*[]TileRequest, error) {
	//TODO: remove commented debug when confident bugs are zapped
	// fmt.Printf("B: %v\n", b)
	z := float64(zoom)

	lonMin := b.MinLong
	for lonMin > 180 {
		lonMin -= 180
	}
	for lonMin < -180 {
		lonMin += 180
	}
	lonMax := b.MaxLong
	for lonMax > 180 {
		lonMax -= 180
	}
	for lonMax < -180 {
		lonMax += 180
	}

	n := math.Exp2(z)
	latMin := math.Min(85.0511, math.Max(-85.0511, b.MinLat)) * math.Pi / 180.0
	latMax := math.Min(85.0511, math.Max(-85.0511, b.MaxLat)) * math.Pi / 180.0

	// fmt.Printf("lon: %v to %v\n", lonMin, lonMax)
	// fmt.Printf("lat: %v to %v\n", latMin, latMax)

	xminf := n * ((lonMin + 180) / 360)
	xmaxf := n * ((lonMax + 180) / 360)
	ymaxf := math.Ceil(n * (1 - (math.Log(math.Tan(latMin)+1.0/math.Cos(latMin)) / math.Pi)) / 2)
	yminf := math.Floor(n * (1 - (math.Log(math.Tan(latMax)+1.0/math.Cos(latMax)) / math.Pi)) / 2)

	ymin := int(math.Min(n, math.Max(0, yminf)))
	ymax := int(math.Min(n, math.Max(0, ymaxf)))
	xmin := int(math.Min(n, math.Max(0, xminf)))
	xmax := int(math.Min(n, math.Max(0, xmaxf)))

	if xmin == xmax {
		xmax = xmin + 1
	}
	if ymin == ymax {
		ymax = ymin + 1
	}

	// fmt.Printf("X : %v to %v \n", xmin, xmax)
	// fmt.Printf("Yf: %v to %v\n", yminf, ymaxf)
	// fmt.Printf("Y : %v to %v\n", ymin, ymax)

	numTiles := (xmax - xmin) * (ymax - ymin)

	if numTiles > 10000 && !force {
		return nil, fmt.Errorf("too many tiles to return (%v > 10000)", numTiles)
	}

	result := make([]TileRequest, numTiles)

	for x := xmin; x < xmax; x++ {
		for y := ymin; y < ymax; y++ {
			result[(y-ymin)*(xmax-xmin)+x-xmin] = TileRequest{layerName, int(zoom), x, y}
		}
	}

	// fmt.Printf("Result: %v\n\n", result)

	return &result, nil
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

func (t TileRequest) String() string {
	return t.LayerName + "/" + strconv.Itoa(t.Z) + "/" + strconv.Itoa(t.X) + "/" + strconv.Itoa(t.Y)
}
