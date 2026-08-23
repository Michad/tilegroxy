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
	"github.com/go-spatial/geom"
	"github.com/go-spatial/geom/encoding/mvt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeCropMvtProviderConfig() map[string]interface{} {
	return map[string]interface{}{
		"name":  "static",
		"image": "embedded:box.mvt",
	}
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

	outTile, err := mvt.DecodeByte(img.Content)
	require.NoError(t, err)
	require.Len(t, outTile.Layers(), 1)
	assert.Len(t, outTile.Layers()[0].Features(), 1)
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

	outTile, err := mvt.DecodeByte(img.Content)
	require.NoError(t, err)
	require.Len(t, outTile.Layers(), 1)
	assert.Len(t, outTile.Layers()[0].Features(), 1)
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

	outTile, err := mvt.DecodeByte(img.Content)
	require.NoError(t, err)
	require.Len(t, outTile.Layers(), 1)
	require.Len(t, outTile.Layers()[0].Features(), 1)

	geo := outTile.Layers()[0].Features()[0].Geometry
	poly, ok := geo.(geom.Polygon)
	require.True(t, ok, "expected a polygon, got %T", geo)

	for _, ring := range poly {
		for _, pt := range ring {
			assert.LessOrEqual(t, pt[0], 2048.0)
		}
	}
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

	outTile, err := mvt.DecodeByte(img.Content)
	require.NoError(t, err)
	require.Len(t, outTile.Layers(), 1)
	assert.Len(t, outTile.Layers()[0].Features(), 1)
}
