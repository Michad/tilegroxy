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
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeFallbackProvidersNoFail() (map[string]interface{}, map[string]interface{}) {
	return map[string]interface{}{
			"name":  "static",
			"color": "F00",
		}, map[string]interface{}{
			"name":  "static",
			"color": "0F0",
		}
}
func makeFallbackProvidersFail() (map[string]interface{}, map[string]interface{}) {
	return map[string]interface{}{
			"name":    "fail",
			"message": "failed intentionally",
		}, map[string]interface{}{
			"name":  "static",
			"color": "0F0",
		}
}

func Test_Fallback_Validate(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := FallbackRegistration{}.Initialize(FallbackConfig{Zoom: "aksfajl", Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)

	assert.Nil(t, f)
	require.Error(t, err)
}

func Test_Fallback_ExecuteNoFallback(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := FallbackRegistration{}.Initialize(FallbackConfig{Zoom: "1-5", Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	assert.NotNil(t, img)
	require.NoError(t, err)
}

func Test_Fallback_ExecuteZoom(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := FallbackRegistration{}.Initialize(FallbackConfig{Zoom: "1-5", Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	exp1, _ := images.GetStaticImage("color:F00")
	exp2, _ := images.GetStaticImage("color:0F0")

	img, err := f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 2, X: 23, Y: 32})

	require.NoError(t, err)
	assert.Equal(t, *exp1, *img)

	img, err = f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 9, X: 23, Y: 32})

	require.NoError(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_ExecuteBounds(t *testing.T) {
	b, _ := pkg.TileRequest{LayerName: "layer", Z: 20, X: 23, Y: 32}.GetBounds()

	p, s := makeFallbackProvidersNoFail()
	f, err := FallbackRegistration{}.Initialize(FallbackConfig{Bounds: *b, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	exp1, _ := images.GetStaticImage("color:F00")
	exp2, _ := images.GetStaticImage("color:0F0")

	img, err := f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 23, Y: 32})

	require.NoError(t, err)
	assert.Equal(t, *exp1, *img)

	img, err = f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})

	require.NoError(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_ExecuteFallback(t *testing.T) {
	p, s := makeFallbackProvidersFail()
	f, err := FallbackRegistration{}.Initialize(FallbackConfig{Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)

	assert.NotNil(t, f)
	require.NoError(t, err)

	exp2, _ := images.GetStaticImage("color:0F0")
	img, err := f.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})

	require.NoError(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_CacheMode(t *testing.T) {
	var err error
	p, s := makeFallbackProvidersFail()

	ctx := pkg.BackgroundContext()
	f, _ := FallbackRegistration{}.Initialize(FallbackConfig{Cache: CacheModeUnlessError, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.True(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Cache: CacheModeAlways, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Cache: CacheModeUnlessFallback, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.True(t, ctx.SkipCacheSave)

	p, s = makeFallbackProvidersNoFail()

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Cache: CacheModeUnlessError, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Cache: CacheModeAlways, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Cache: CacheModeUnlessFallback, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Zoom: "1-5", Cache: CacheModeUnlessError, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Zoom: "1-5", Cache: CacheModeAlways, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = FallbackRegistration{}.Initialize(FallbackConfig{Zoom: "1-5", Cache: CacheModeUnlessFallback, Primary: p, Secondary: s}, config.ClientConfig{}, testErrMessages, nil)
	_, err = f.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	require.NoError(t, err)
	assert.True(t, ctx.SkipCacheSave)
}
