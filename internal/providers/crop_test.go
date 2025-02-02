// Copyright 2024 Michael Davis
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
	"bytes"
	"image"
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeCropProvidersImages() (map[string]interface{}, map[string]interface{}, map[string]interface{}) {
	return map[string]interface{}{
			"name":  "static",
			"image": "test_files/10_pixel_blue.png",
		}, map[string]interface{}{
			"name":  "static",
			"image": "test_files/single_pixel_red.png",
		}, map[string]interface{}{
			"name":  "static",
			"image": "test_files/10_pixel_red_blue.png",
		}
}

func Test_Crop_ExecuteNoCrop(t *testing.T) {
	p, s, _ := makeCropProvidersImages()
	f, err := CropRegistration{}.Initialize(CropConfig{Bounds: pkg.Bounds{South: -90, North: 90, West: 0, East: 180}, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 1, X: 0, Y: 0})

	assert.NotNil(t, img)
	require.NoError(t, err)

	exp, err := images.GetStaticImage(s["image"].(string))
	require.NoError(t, err)

	img1, _, err := image.Decode(bytes.NewReader(img.Content))
	require.NoError(t, err)
	img2, _, err := image.Decode(bytes.NewReader(*exp))
	require.NoError(t, err)
	assert.Equal(t, img1, img2)
}

func Test_Crop_ExecuteCrop(t *testing.T) {
	p, s, ps := makeCropProvidersImages()
	f, err := CropRegistration{}.Initialize(CropConfig{Bounds: pkg.Bounds{South: -90, North: 90, West: 0, East: 180}, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assert.NotNil(t, img)
	require.NoError(t, err)

	exp, err := images.GetStaticImage(ps["image"].(string))
	require.NoError(t, err)

	img1, _, err := image.Decode(bytes.NewReader(img.Content))
	require.NoError(t, err)
	img2, _, err := image.Decode(bytes.NewReader(*exp))
	require.NoError(t, err)
	assert.Equal(t, img1, img2)
}

func Test_Crop_ExecuteCropReverseOrder(t *testing.T) {
	s, p, ps := makeCropProvidersImages()
	f, err := CropRegistration{}.Initialize(CropConfig{Bounds: pkg.Bounds{South: -90, North: 90, West: -180, East: 0}, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assert.NotNil(t, img)
	require.NoError(t, err)

	exp, err := images.GetStaticImage(ps["image"].(string))
	require.NoError(t, err)

	img1, _, err := image.Decode(bytes.NewReader(img.Content))
	require.NoError(t, err)
	img2, _, err := image.Decode(bytes.NewReader(*exp))
	require.NoError(t, err)
	assert.Equal(t, img1, img2)
}

func Test_Crop_ExecuteCropWithAuth(t *testing.T) {
	p, s, ps := makeCropProvidersImages()
	f, err := CropRegistration{}.Initialize(CropConfig{Primary: p, Secondary: s, BoundsFromAuth: true}, config.ClientConfig{}, testErrMessages, nil, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()
	b, _ := pkg.AllowedAreaFromContext(ctx)
	*b = pkg.Bounds{South: -90, North: 90, West: 0, East: 180}
	img, err := f.GenerateTile(ctx, pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assert.NotNil(t, img)
	require.NoError(t, err)

	exp, err := images.GetStaticImage(ps["image"].(string))
	require.NoError(t, err)

	img1, _, err := image.Decode(bytes.NewReader(img.Content))
	require.NoError(t, err)
	img2, _, err := image.Decode(bytes.NewReader(*exp))
	require.NoError(t, err)
	assert.Equal(t, img1, img2)
}

func Test_Crop_ExecuteCropNoBounds(t *testing.T) {
	p, s, _ := makeCropProvidersImages()
	cfg := CropRegistration{}.InitializeConfig().(CropConfig)
	cfg.Primary = p
	cfg.Secondary = s
	f, err := CropRegistration{}.Initialize(cfg, config.ClientConfig{}, testErrMessages, nil, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0})

	assert.NotNil(t, img)
	require.NoError(t, err)

	exp, err := images.GetStaticImage(p["image"].(string))
	require.NoError(t, err)

	img1, _, err := image.Decode(bytes.NewReader(img.Content))
	require.NoError(t, err)
	img2, _, err := image.Decode(bytes.NewReader(*exp))
	require.NoError(t, err)
	assert.Equal(t, img1, img2)
}
