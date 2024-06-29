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
	"errors"
	"testing"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/stretchr/testify/assert"
)

type Fail struct {
}

func (t Fail) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return providerContext, errors.New("failed intentionally")
}

func (t Fail) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	return nil, errors.New("failed intentionally")
}

func makeFallbackProvidersNoFail() (*Provider, *Provider) {
	a, _ := ConstructStatic(StaticConfig{Color: "F00"}, nil, &testErrMessages)
	b, _ := ConstructStatic(StaticConfig{Color: "0F0"}, nil, &testErrMessages)
	var ap Provider = *a
	var bp Provider = *b

	return &ap, &bp
}
func makeFallbackProvidersFail() (*Provider, *Provider) {
	b, _ := ConstructStatic(StaticConfig{Color: "0F0"}, nil, &testErrMessages)
	var ap Provider = Fail{}
	var bp Provider = *b

	return &ap, &bp
}

func Test_Fallback_Validate(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Zoom: "aksfajl"}, &config.ClientConfig{}, &testErrMessages, p, s)

	assert.Nil(t, f)
	assert.NotNil(t, err)
}

func Test_Fallback_ExecuteNoFallback(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Zoom: "1-5"}, &config.ClientConfig{}, &testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.Nil(t, err)

	pc, err := f.PreAuth(internal.BackgroundContext(), ProviderContext{})
	assert.NotNil(t, pc)
	assert.Nil(t, err)

	img, err := f.GenerateTile(internal.BackgroundContext(), pc, internal.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	assert.NotNil(t, img)
	assert.Nil(t, err)
}

func Test_Fallback_ExecuteZoom(t *testing.T) {
	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Zoom: "1-5"}, &config.ClientConfig{}, &testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.Nil(t, err)

	exp1, _ := images.GetStaticImage("color:F00")
	exp2, _ := images.GetStaticImage("color:0F0")

	img, err := f.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 2, X: 23, Y: 32})

	assert.Nil(t, err)
	assert.Equal(t, *exp1, *img)

	img, err = f.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 9, X: 23, Y: 32})

	assert.Nil(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_ExecuteBouns(t *testing.T) {
	b, _ := internal.TileRequest{LayerName: "layer", Z: 20, X: 23, Y: 32}.GetBounds()

	p, s := makeFallbackProvidersNoFail()
	f, err := ConstructFallback(FallbackConfig{Bounds: *b}, &config.ClientConfig{}, &testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.Nil(t, err)

	exp1, _ := images.GetStaticImage("color:F00")
	exp2, _ := images.GetStaticImage("color:0F0")

	img, err := f.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 20, X: 23, Y: 32})

	assert.Nil(t, err)
	assert.Equal(t, *exp1, *img)

	img, err = f.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})

	assert.Nil(t, err)
	assert.Equal(t, *exp2, *img)
}

func Test_Fallback_ExecuteFallback(t *testing.T) {
	p, s := makeFallbackProvidersFail()
	f, err := ConstructFallback(FallbackConfig{}, &config.ClientConfig{}, &testErrMessages, p, s)

	assert.NotNil(t, f)
	assert.Nil(t, err)

	exp2, _ := images.GetStaticImage("color:0F0")
	img, err := f.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 20, X: 1, Y: 1})

	assert.Nil(t, err)
	assert.Equal(t, *exp2, *img)
}
