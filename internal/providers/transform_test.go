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
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_Transform(t *testing.T) {
	assert.Equal(t, config.DataTypeRaster, TransformRegistration{}.DataType(TransformConfig{}))
}

func makeTransformProvider() map[string]interface{} {
	return map[string]interface{}{
		"name":  "static",
		"color": "F00",
	}
}

func Test_Transform_Validate(t *testing.T) {
	p := makeTransformProvider()
	tr, err := TransformRegistration{}.Initialize(TransformConfig{Provider: p}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, tr)
	require.Error(t, err)
	tr, err = TransformRegistration{}.Initialize(TransformConfig{Formula: "package custom", Provider: p}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, tr)
	require.Error(t, err)
}

func Test_Transform_WrongSignatureFailsAtInitialize(t *testing.T) {
	p := makeTransformProvider()
	tr, err := TransformRegistration{}.Initialize(TransformConfig{Provider: p, Formula: `func transform(r, g, b, a uint8) uint8 { return r }`}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, tr)
	require.Error(t, err)
}

func Test_Transform_Execute(t *testing.T) {
	p := makeTransformProvider()
	tr, err := TransformRegistration{}.Initialize(TransformConfig{Provider: p, Formula: `func transform(r, g, b, a uint8) (uint8, uint8, uint8, uint8) { return g,b,r,a }`}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.NotNil(t, tr)
	require.NoError(t, err)

	exp, _ := images.GetStaticImage("color:00F")

	pc, err := tr.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := tr.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	require.NoError(t, err)
	assert.Equal(t, *exp, img.Content)
}

// Writes a solid-color single pixel image to a temp file and returns its path.
func writeTestImage(t *testing.T, name string, col color.Color, encode func(*os.File, image.Image) error) string {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, col)

	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, encode(f, img))
	require.NoError(t, f.Close())

	return path
}

func transformedPixel(t *testing.T, imagePath string, formula string) color.NRGBA {
	t.Helper()

	tr, err := TransformRegistration{}.Initialize(
		TransformConfig{Provider: map[string]interface{}{"name": "static", "image": imagePath}, Formula: formula},
		layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	require.NoError(t, err)

	img, err := tr.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})
	require.NoError(t, err)

	result, err := png.Decode(bytes.NewReader(img.Content))
	require.NoError(t, err)

	return color.NRGBAModel.Convert(result.At(0, 0)).(color.NRGBA)
}

func Test_Transform_JPEGChannelsArePassedThroughUnchanged(t *testing.T) {
	path := writeTestImage(t, "src.jpg", color.NRGBA{R: 18, G: 52, B: 86, A: 255}, func(f *os.File, img image.Image) error {
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 100})
	})

	// JPEG is lossy, so assert the identity transform preserves whatever the decoder produced.
	expected := transformedPixel(t, path, `func transform(r, g, b, a uint8) (uint8, uint8, uint8, uint8) { return r,g,b,a }`)
	swapped := transformedPixel(t, path, `func transform(r, g, b, a uint8) (uint8, uint8, uint8, uint8) { return b,r,g,a }`)

	assert.Equal(t, color.NRGBA{R: expected.B, G: expected.R, B: expected.G, A: 255}, swapped)
	assert.InDelta(t, 18, int(expected.R), 8)
	assert.InDelta(t, 52, int(expected.G), 8)
	assert.InDelta(t, 86, int(expected.B), 8)
}

func Test_Transform_SemiTransparentPixelKeepsItsChannels(t *testing.T) {
	path := writeTestImage(t, "src.png", color.NRGBA{R: 200, G: 100, B: 50, A: 128}, func(f *os.File, img image.Image) error {
		return png.Encode(f, img)
	})

	result := transformedPixel(t, path, `func transform(r, g, b, a uint8) (uint8, uint8, uint8, uint8) { return r,g,b,a }`)

	assert.Equal(t, color.NRGBA{R: 200, G: 100, B: 50, A: 128}, result)
}
