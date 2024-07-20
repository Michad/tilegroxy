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

package layers

import (
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/stretchr/testify/assert"
)

func makeFallbackProvidersNoFail() (entities.Provider, entities.Provider) {
	a, _ := ConstructStatic(StaticConfig{Color: "F00"}, testClientConfig, testErrMessages)
	b, _ := ConstructStatic(StaticConfig{Color: "0F0"}, testClientConfig, testErrMessages)

	return a, b
}
func makeFallbackProvidersFail() (entities.Provider, entities.Provider) {
	b, _ := ConstructStatic(StaticConfig{Color: "0F0"}, testClientConfig, testErrMessages)
	var ap entities.Provider = Fail{FailConfig{Message: "failed intentionally"}}

	return ap, b
}

func Test_Fallback_Validate(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Zoom: "aksfajl"}, config.ClientConfig{}, testErrMessages, p, s)

	assert.Nil(t, f)
	assert.Error(t, err)
}

func Test_Fallback_ExecuteNoFallback(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Zoom: "1-5"}, config.ClientConfig{}, testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.NoError(t, err)

	pc, err := f.PreAuth(pkg.BackgroundContext(), entities.ProviderContext{})
	assert.NotNil(t, pc)
	assert.NoError(t, err)

	img, err := f.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	assert.NotNil(t, img)
	assert.NoError(t, err)
}

func Test_Fallback_ExecuteZoom(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Zoom: "1-5"}, config.ClientConfig{}, testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.NoError(t, err)

	exp1, _ := images.GetStaticImage("color:F00")
	exp2, _ := images.GetStaticImage("color:0F0")

	img, err := f.GenerateTile(pkg.BackgroundContext(), entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 2, X: 23, Y: 32})

	assert.NoError(t, err)
	assert.Equal(t, *exp1, *img)

	img, err = f.GenerateTile(pkg.BackgroundContext(), entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 9, X: 23, Y: 32})

	assert.NoError(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_ExecuteBounds(t *testing.T) {
	b, _ := pkg.TileRequest{LayerName: "layer", Z: 20, X: 23, Y: 32}.GetBounds()

	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Bounds: *b}, config.ClientConfig{}, testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.NoError(t, err)

	exp1, _ := images.GetStaticImage("color:F00")
	exp2, _ := images.GetStaticImage("color:0F0")

	img, err := f.GenerateTile(pkg.BackgroundContext(), entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 23, Y: 32})

	assert.NoError(t, err)
	assert.Equal(t, *exp1, *img)

	img, err = f.GenerateTile(pkg.BackgroundContext(), entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})

	assert.NoError(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_ExecuteFallback(t *testing.T) {
	p, s := makeFallbackProvidersFail()
	f, err := ConstructFallback(FallbackConfig{}, config.ClientConfig{}, testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.NoError(t, err)

	exp2, _ := images.GetStaticImage("color:0F0")
	img, err := f.GenerateTile(pkg.BackgroundContext(), entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})

	assert.NoError(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_CacheMode(t *testing.T) {
	p, s := makeFallbackProvidersFail()

	ctx := pkg.BackgroundContext()
	f, _ := ConstructFallback(FallbackConfig{Cache: CacheModeUnlessError}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.True(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Cache: CacheModeAlways}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Cache: CacheModeUnlessFallback}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.True(t, ctx.SkipCacheSave)

	p, s = makeFallbackProvidersNoFail()

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Cache: CacheModeUnlessError}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Cache: CacheModeAlways}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Cache: CacheModeUnlessFallback}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Zoom: "1-5", Cache: CacheModeUnlessError}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Zoom: "1-5", Cache: CacheModeAlways}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.False(t, ctx.SkipCacheSave)

	ctx = pkg.BackgroundContext()
	f, _ = ConstructFallback(FallbackConfig{Zoom: "1-5", Cache: CacheModeUnlessFallback}, config.ClientConfig{}, testErrMessages, p, s)
	f.GenerateTile(ctx, entities.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})
	assert.True(t, ctx.SkipCacheSave)
}
