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

package providers

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/internal/mlt"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/paulmach/orb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_CropMlt(t *testing.T) {
	assert.Equal(t, config.DataTypeMLT, CropMltRegistration{}.DataType(CropMltConfig{}))
}

func makeCropMltProviderConfig() map[string]interface{} {
	return map[string]interface{}{
		"name":  "static",
		"image": "embedded:box.mlt",
	}
}

func generateCropMlt(ctx context.Context, t *testing.T, cfg CropMltConfig, tile pkg.TileRequest) *pkg.Image {
	t.Helper()

	f, err := CropMltRegistration{}.Initialize(cfg, layer.ProviderDeps{ErrorMessages: testErrMessages})
	require.NoError(t, err)

	pc, err := f.PreAuth(ctx, layer.ProviderContext{})
	require.NoError(t, err)

	img, err := f.GenerateTile(ctx, pc, tile)
	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, "application/vnd.maplibre-tile", img.ContentType)

	return img
}

// embedded:box.mlt, like box.mvt, fills the full 0-4096 extent of whatever tile it's served for.
func cropMltBound(ctx context.Context, t *testing.T, cfg CropMltConfig, tile pkg.TileRequest) orb.Bound {
	t.Helper()

	img := generateCropMlt(ctx, t, cfg, tile)

	layers, skipped, err := mlt.Decode(img.Content)
	require.NoError(t, err)
	assert.Zero(t, skipped)
	require.Len(t, layers, 1)
	require.Equal(t, 1, layers[0].FeatureCount())

	poly, ok := layers[0].Geometries[0].(orb.Polygon)
	require.True(t, ok, "expected a polygon, got %T", layers[0].Geometries[0])

	return poly.Bound()
}

// The same cases as cropmvt, so the two formats are held to the same crop.
func Test_CropMlt_ExecuteCrop(t *testing.T) {
	z0 := pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0}
	z1 := pkg.TileRequest{LayerName: "l", Z: 1, X: 0, Y: 0}

	tests := map[string]struct {
		bounds pkg.Bounds
		tile   pkg.TileRequest
		want   orb.Bound
	}{
		"no bounds":                     {pkg.Bounds{}, z0, orb.Bound{Max: orb.Point{4096, 4096}}},
		"whole world":                   {pkg.Bounds{South: -90, North: 90, West: -180, East: 180}, z0, orb.Bound{Max: orb.Point{4096, 4096}}},
		"west half":                     {pkg.Bounds{South: -90, North: 90, West: -180, East: 0}, z0, orb.Bound{Max: orb.Point{2048, 4096}}},
		"inside the tile":               {pkg.Bounds{South: -quarterMercatorLat, North: quarterMercatorLat, West: -90, East: 90}, z0, orb.Bound{Min: orb.Point{1024, 1024}, Max: orb.Point{3072, 3072}}},
		"tile inside crop":              {pkg.Bounds{South: -90, North: 90, West: -180, East: 180}, z1, orb.Bound{Max: orb.Point{4096, 4096}}},
		"equals the tile":               {pkg.Bounds{South: 0, North: maxMercatorLat, West: -180, East: 0}, z1, orb.Bound{Max: orb.Point{4096, 4096}}},
		"northeast corner":              {pkg.Bounds{South: 0, North: 90, West: 0, East: 180}, z0, orb.Bound{Min: orb.Point{2048, 0}, Max: orb.Point{4096, 2048}}},
		"longitude within mercator":     {pkg.Bounds{South: -maxMercatorLat, North: maxMercatorLat, West: -90, East: 90}, z0, orb.Bound{Min: orb.Point{1024, 0}, Max: orb.Point{3072, 4096}}},
		"west half of the quadrant":     {pkg.Bounds{South: 0, North: 45, West: -180, East: -90}, z1, orb.Bound{Min: orb.Point{0, latToTileY(t, 45, 1, 0)}, Max: orb.Point{2048, 4096}}},
		"beyond the mercator latitudes": {pkg.Bounds{South: -90, North: 89, West: 0, East: 180}, z0, orb.Bound{Min: orb.Point{2048, 0}, Max: orb.Point{4096, 4096}}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			b := cropMltBound(pkg.BackgroundContext(), t, CropMltConfig{Bounds: tc.bounds, Primary: makeCropMltProviderConfig()}, tc.tile)
			assertBound(t, tc.want.Min[0], tc.want.Min[1], tc.want.Max[0], tc.want.Max[1], b)
		})
	}
}

func Test_CropMlt_ExecuteCropOutside(t *testing.T) {
	img := generateCropMlt(pkg.BackgroundContext(), t, CropMltConfig{Bounds: pkg.Bounds{South: -1, North: 1, West: -1, East: 1}, Primary: makeCropMltProviderConfig()}, pkg.TileRequest{LayerName: "l", Z: 10, X: 1, Y: 1})

	assert.Empty(t, img.Content)
	assert.True(t, img.ForceSkipCache)
}

type fixedImageProvider struct {
	img pkg.Image
}

func (p fixedImageProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	return pc, nil
}

func (p fixedImageProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	img := p.img
	return &img, nil
}

// A layer clipped down to nothing is left out, as is a layer in a format that can't be clipped.
func Test_CropMlt_ExecuteCropDropsLayers(t *testing.T) {
	tile, err := mlt.Encode([]mlt.Layer{
		{Name: "northwest", Extent: 4096, Geometries: []orb.Geometry{orb.Point{100, 100}}},
		{Name: "southeast", Extent: 4096, Geometries: []orb.Geometry{orb.Point{4000, 4000}}},
	})
	require.NoError(t, err)

	futureLayer := []byte{3, 2, 0xAA, 0xBB}
	c := CropMlt{
		CropMltConfig: CropMltConfig{Bounds: pkg.Bounds{South: 0, North: 90, West: -180, East: 0}},
		Primary:       fixedImageProvider{pkg.Image{Content: append(tile, futureLayer...), ContentType: mltContentType}},
		errorMessages: testErrMessages,
	}

	img, err := c.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	require.NoError(t, err)

	layers, skipped, err := mlt.Decode(img.Content)
	require.NoError(t, err)
	assert.Zero(t, skipped)
	require.Len(t, layers, 1)
	assert.Equal(t, "northwest", layers[0].Name)
}

func Test_CropMlt_ExecuteCropWithAuth(t *testing.T) {
	ctx := pkg.BackgroundContext()
	b, _ := pkg.AllowedAreaFromContext(ctx)
	*b = pkg.Bounds{South: -90, North: 90, West: -180, East: 0}

	bound := cropMltBound(ctx, t, CropMltConfig{Primary: makeCropMltProviderConfig(), BoundsFromAuth: true}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assertBound(t, 0, 0, 2048, 4096, bound)
}

func Test_CropMlt_RejectsInvalidTiles(t *testing.T) {
	f, err := CropMltRegistration{}.Initialize(CropMltConfig{Bounds: pkg.Bounds{South: -90, North: 90, West: -180, East: 0}, Primary: map[string]interface{}{
		"name": "static", "image": "embedded:transparent.png",
	}}, layer.ProviderDeps{ErrorMessages: testErrMessages})
	require.NoError(t, err)

	_, err = f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	require.Error(t, err)

	_, err = f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 99, X: 0, Y: 0})
	require.Error(t, err)
}

func Test_CropMlt_ExecutePrimaryFailure(t *testing.T) {
	f, err := CropMltRegistration{}.Initialize(CropMltConfig{Primary: map[string]interface{}{"name": "fail"}}, layer.ProviderDeps{ErrorMessages: testErrMessages})
	require.NoError(t, err)

	_, err = f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	require.Error(t, err)

	c := CropMlt{Primary: nilProvider{}, errorMessages: testErrMessages}
	_, err = c.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	require.Error(t, err)
}

// Cropping one vector format with the other's provider fails on every partially covered tile.
func Test_Crop_RejectsOtherVectorFormat(t *testing.T) {
	deps := layer.ProviderDeps{ErrorMessages: testErrMessages}

	_, err := CropMltRegistration{}.Initialize(CropMltConfig{Primary: makeCropMvtProviderConfig()}, deps)
	require.Error(t, err)

	_, err = CropMvtRegistration{}.Initialize(CropMvtConfig{Primary: makeCropMltProviderConfig()}, deps)
	require.Error(t, err)

	_, err = CropMltRegistration{}.Initialize(CropMltConfig{Primary: map[string]interface{}{"name": "nope"}}, deps)
	require.Error(t, err)
}

func Test_CropMlt_WrapBounds(t *testing.T) {
	inner, err := StaticRegistration{}.Initialize(StaticConfig{Image: images.KeyMltBox}, layer.ProviderDeps{ErrorMessages: testErrMessages})
	require.NoError(t, err)

	p, err := CropMltRegistration{}.WrapBounds(inner, pkg.Bounds{South: -90, North: 90, West: -180, East: 0}, layer.ProviderDeps{ErrorMessages: testErrMessages})
	require.NoError(t, err)

	pc, err := p.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	require.NoError(t, err)
	assert.True(t, pc.AuthBypass)

	img, err := p.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})
	require.NoError(t, err)
	assert.NotEqual(t, []byte{}, img.Content)
}

func Test_CropMlt_ForwardsPrimaryMetadata(t *testing.T) {
	md := layer.Description{TileJSONMetadata: config.TileJSONMetadata{Description: "from primary"}}

	assert.Equal(t, md, CropMlt{Primary: metadataOnlyProvider{md: md}}.Metadata())
}

func Test_CropMlt_ClosesPrimary(t *testing.T) {
	var closed bool

	require.NoError(t, layer.CloseProvider(context.Background(), CropMlt{Primary: closeRecordingProvider{closed: &closed}}))
	assert.True(t, closed)
}

type nilProvider struct{}

func (nilProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	return pc, nil
}

func (nilProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}
