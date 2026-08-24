// Copyright 2025 Michael Davis
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

package providers

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_CropMvt(t *testing.T) {
	assert.Equal(t, pkg.DataTypeMVT, CropMvtRegistration{}.DataType(CropMvtConfig{}))
}

func makeCropMvtProviderConfig() map[string]interface{} {
	return map[string]interface{}{
		"name":  "static",
		"image": "embedded:box.mvt",
	}
}

// The maximum latitude representable in web mercator, the north/south edge of the z0 tile.
const maxMercatorLat = 85.0511287798066

// embedded:box.mvt holds a single polygon filling the full 0-4096 extent of whatever tile it's
// served for, so the bound of the output polygon shows exactly what the crop kept.
func cropMvtBound(t *testing.T, bounds pkg.Bounds, tile pkg.TileRequest) orb.Bound {
	t.Helper()

	f, err := CropMvtRegistration{}.Initialize(CropMvtConfig{Bounds: bounds, Primary: makeCropMvtProviderConfig()}, layer.ProviderDeps{ErrorMessages: testErrMessages})
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, tile)
	require.NoError(t, err)
	require.NotNil(t, img)

	outLayers, err := mvt.Unmarshal(img.Content)
	require.NoError(t, err)
	require.Len(t, outLayers, 1)
	require.Len(t, outLayers[0].Features, 1)

	geo := outLayers[0].Features[0].Geometry
	poly, ok := geo.(orb.Polygon)
	require.True(t, ok, "expected a polygon, got %T", geo)

	return poly.Bound()
}

// Projects a latitude into the y coordinate of a tile's 0-4096 extent, so expectations follow
// the same web mercator math as the code rather than a hand computed constant. Y grows southward.
func latToTileY(t *testing.T, lat float64, z, y int) float64 {
	t.Helper()

	tb, err := pkg.TileRequest{LayerName: "l", Z: z, X: 0, Y: y}.GetBoundsProjection(pkg.SRIDPsuedoMercator)
	require.NoError(t, err)

	m := pkg.Bounds{South: lat, North: lat, West: -180, East: 180}.ConvertToPsuedoMercatorRange()

	return (tb.North - m.North) / (tb.North - tb.South) * 4096
}

func assertBound(t *testing.T, expMinX, expMinY, expMaxX, expMaxY float64, actual orb.Bound) {
	t.Helper()

	assert.InDelta(t, expMinX, actual.Min[0], 1, "min x")
	assert.InDelta(t, expMinY, actual.Min[1], 1, "min y")
	assert.InDelta(t, expMaxX, actual.Max[0], 1, "max x")
	assert.InDelta(t, expMaxY, actual.Max[1], 1, "max y")
}

func Test_CropMvt_ExecuteNoCrop(t *testing.T) {
	p := makeCropMvtProviderConfig()
	f, err := CropMvtRegistration{}.Initialize(CropMvtConfig{Bounds: pkg.Bounds{South: -90, North: 90, West: -180, East: 180}, Primary: p}, layer.ProviderDeps{ErrorMessages: testErrMessages})

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	assert.NotNil(t, img)
	require.NoError(t, err)

	outLayers, err := mvt.Unmarshal(img.Content)
	require.NoError(t, err)
	require.Len(t, outLayers, 1)
	require.Len(t, outLayers[0].Features, 1)

	poly, ok := outLayers[0].Features[0].Geometry.(orb.Polygon)
	require.True(t, ok, "expected a polygon, got %T", outLayers[0].Features[0].Geometry)
	assertBound(t, 0, 0, 4096, 4096, poly.Bound())
}

func Test_CropMvt_ExecuteNoBounds(t *testing.T) {
	p := makeCropMvtProviderConfig()
	cfg := CropMvtRegistration{}.InitializeConfig().(CropMvtConfig)
	cfg.Primary = p
	f, err := CropMvtRegistration{}.Initialize(cfg, layer.ProviderDeps{ErrorMessages: testErrMessages})

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	assert.NotNil(t, img)
	require.NoError(t, err)

	outLayers, err := mvt.Unmarshal(img.Content)
	require.NoError(t, err)
	require.Len(t, outLayers, 1)
	require.Len(t, outLayers[0].Features, 1)

	poly, ok := outLayers[0].Features[0].Geometry.(orb.Polygon)
	require.True(t, ok, "expected a polygon, got %T", outLayers[0].Features[0].Geometry)
	assertBound(t, 0, 0, 4096, 4096, poly.Bound())
}

func Test_CropMvt_ExecuteCropHalf(t *testing.T) {
	p := makeCropMvtProviderConfig()
	// Crops the west half of the world, which is also the west half of tile z0/x0/y0
	f, err := CropMvtRegistration{}.Initialize(CropMvtConfig{Bounds: pkg.Bounds{South: -90, North: 90, West: -180, East: 0}, Primary: p}, layer.ProviderDeps{ErrorMessages: testErrMessages})

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	assert.NotNil(t, img)
	require.NoError(t, err)

	outLayers, err := mvt.Unmarshal(img.Content)
	require.NoError(t, err)
	require.Len(t, outLayers, 1)
	require.Len(t, outLayers[0].Features, 1)

	geo := outLayers[0].Features[0].Geometry
	poly, ok := geo.(orb.Polygon)
	require.True(t, ok, "expected a polygon, got %T", geo)

	// The west half of the world keeps x 0-2048 and the full y range
	assertBound(t, 0, 0, 2048, 4096, poly.Bound())
}

func Test_CropMvt_ExecuteCropOutside(t *testing.T) {
	p := makeCropMvtProviderConfig()
	// Bounds fully outside tile z0/x0/y0, which covers the whole world
	f, err := CropMvtRegistration{}.Initialize(CropMvtConfig{Bounds: pkg.Bounds{South: -1, North: 1, West: -1, East: 1}, Primary: p}, layer.ProviderDeps{ErrorMessages: testErrMessages})

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 20, X: 0, Y: 0})
	assert.NotNil(t, img)
	require.NoError(t, err)
	assert.Empty(t, img.Content)
}

// The latitude falling on the 25%/75% lines of a z0 tile, atan(sinh(pi/2)) in degrees. Latitude
// is not linear in the y axis under web mercator, so this is not 45.
const quarterMercatorLat = 66.51326044311186

// Crop bounds (A) strictly inside the tile bounds (B), which cover the whole world at z0/x0/y0.
// Longitude +-90 is a quarter in from each side on x, and the latitude above matches that on y.
func Test_CropMvt_ExecuteCropBoundsInsideTile(t *testing.T) {
	b := cropMvtBound(t, pkg.Bounds{South: -quarterMercatorLat, North: quarterMercatorLat, West: -90, East: 90}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assertBound(t, 1024, 1024, 3072, 3072, b)
}

// Tile bounds (B) at z1/x0/y0 strictly inside the crop bounds (A), which cover the whole world.
// Nothing is clipped away, so the full extent survives.
func Test_CropMvt_ExecuteTileInsideCropBounds(t *testing.T) {
	b := cropMvtBound(t, pkg.Bounds{South: -90, North: 90, West: -180, East: 180}, pkg.TileRequest{LayerName: "l", Z: 1, X: 0, Y: 0})

	assertBound(t, 0, 0, 4096, 4096, b)
}

// Crop bounds (A) exactly equal the tile bounds (B) at z1/x0/y0, so the whole tile survives.
func Test_CropMvt_ExecuteCropBoundsEqualsTileBounds(t *testing.T) {
	b := cropMvtBound(t, pkg.Bounds{South: 0, North: maxMercatorLat, West: -180, East: 0}, pkg.TileRequest{LayerName: "l", Z: 1, X: 0, Y: 0})

	assertBound(t, 0, 0, 4096, 4096, b)
}

// Crop bounds (A) overlap only the northeast quadrant of tile z0/x0/y0, keeping the east half
// on x and the north half on y. MVT y grows southward, so north is the low half.
func Test_CropMvt_ExecuteCropPartialOverlapCorner(t *testing.T) {
	b := cropMvtBound(t, pkg.Bounds{South: 0, North: 90, West: 0, East: 180}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assertBound(t, 2048, 0, 4096, 2048, b)
}

// A crop that trims only longitude must still apply when its latitude range sits at or inside
// the web mercator limit rather than being treated as covering the tile.
func Test_CropMvt_ExecuteCropLongitudeOnlyWithinMercatorLimit(t *testing.T) {
	b := cropMvtBound(t, pkg.Bounds{South: -maxMercatorLat, North: maxMercatorLat, West: -90, East: 90}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assertBound(t, 1024, 0, 3072, 4096, b)
}

// The west half of the north-west quadrant tile, using a crop well inside the mercator limit.
// The z1 tile spans the equator to the mercator limit, so latitude 45 lands partway down it.
func Test_CropMvt_ExecuteCropWestHalfOfQuadrantTile(t *testing.T) {
	b := cropMvtBound(t, pkg.Bounds{South: 0, North: 45, West: -180, East: -90}, pkg.TileRequest{LayerName: "l", Z: 1, X: 0, Y: 0})

	assertBound(t, 0, latToTileY(t, 45, 1, 0), 2048, 4096, b)
}

func Test_CropMvt_ExecuteCropWithAuth(t *testing.T) {
	p := makeCropMvtProviderConfig()
	f, err := CropMvtRegistration{}.Initialize(CropMvtConfig{Primary: p, BoundsFromAuth: true}, layer.ProviderDeps{ErrorMessages: testErrMessages})

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()
	b, _ := pkg.AllowedAreaFromContext(ctx)
	*b = pkg.Bounds{South: -90, North: 90, West: -180, East: 0}
	img, err := f.GenerateTile(ctx, pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assert.NotNil(t, img)
	require.NoError(t, err)

	outLayers, err := mvt.Unmarshal(img.Content)
	require.NoError(t, err)
	require.Len(t, outLayers, 1)
	require.Len(t, outLayers[0].Features, 1)

	poly, ok := outLayers[0].Features[0].Geometry.(orb.Polygon)
	require.True(t, ok, "expected a polygon, got %T", outLayers[0].Features[0].Geometry)
	// The bounds from auth crop to the west half of the world
	assertBound(t, 0, 0, 2048, 4096, poly.Bound())
}
