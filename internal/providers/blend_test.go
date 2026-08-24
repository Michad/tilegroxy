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
	"context"
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type closableProvider struct {
	closed bool
}

func (p *closableProvider) Close(_ context.Context) error {
	p.closed = true
	return nil
}

func (p *closableProvider) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{}, nil
}

func (p *closableProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (p *closableProvider) DataType() pkg.DataType {
	return pkg.DataTypeUnknown
}

func Test_DataType_Blend(t *testing.T) {
	assert.Equal(t, pkg.DataTypeRaster, BlendRegistration{}.DataType(BlendConfig{}))
}

// Blend holds its children directly, outside any layer, so they're unreachable through
// LayerGroup.Close unless Blend forwards to them itself.
func Test_BlendCloseClosesChildProviders(t *testing.T) {
	p1 := &closableProvider{}
	p2 := &closableProvider{}
	// ConstructProvider always wraps, so one child is wrapped here to match what production
	// actually builds: the close has to survive both hops, not just the forwarding one
	b := &Blend{providers: []layer.Provider{layer.ProviderWrapper{Name: "child", Provider: p1}, p2}}

	require.NoError(t, b.Close(context.Background()))

	assert.True(t, p1.closed)
	assert.True(t, p2.closed)
}

func makeBlendProviders() []map[string]interface{} {
	return []map[string]interface{}{{
		"name":  "static",
		"color": "F00",
	}, {
		"name":  "static",
		"color": "0F0",
	},
	}
}

func Test_BlendValidate(t *testing.T) {
	providers := makeBlendProviders()
	b, err := BlendRegistration{}.Initialize(BlendConfig{Providers: providers}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	assert.Nil(t, b)
	require.Error(t, err)
	b, err = BlendRegistration{}.Initialize(BlendConfig{Mode: "fake", Providers: providers}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	assert.Nil(t, b)
	require.Error(t, err)
	b, err = BlendRegistration{}.Initialize(BlendConfig{Mode: "add", Opacity: 23, Providers: providers}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	assert.Nil(t, b)
	require.Error(t, err)
	b, err = BlendRegistration{}.Initialize(BlendConfig{Mode: "opacity", Opacity: 23, Providers: []map[string]interface{}{}}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	assert.Nil(t, b)
	require.Error(t, err)
}

func Test_Blend_Layers(t *testing.T) {
	v1 := make(map[string]string)
	v2 := make(map[string]string)
	v1["a"] = "hello"
	v1["b"] = "world"
	v2["a"] = "goodbye"
	v2["b"] = "world"

	b, err := BlendRegistration{}.Initialize(BlendConfig{
		Providers: makeBlendProviders(),
		Mode:      "normal",
		Layer: &BlendLayerConfig{
			Pattern: "something_{a}_{b}",
			Values:  []map[string]string{v1, v2},
		}}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	assert.NotNil(t, b)
	require.NoError(t, err)
	bb := b.(*Blend)

	assert.Len(t, bb.providers, 2)
	// assert.Equal(t, &Ref{RefConfig{"something_hello_world"}, nil}, bb.providers[0])
	// assert.Equal(t, &Ref{RefConfig{"something_goodbye_world"}, nil}, bb.providers[1])
}

func Test_BlendExecute_Add(t *testing.T) {
	b, err := BlendRegistration{}.Initialize(BlendConfig{Mode: "add", Providers: makeBlendProviders()}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	assert.NotNil(t, b)
	require.NoError(t, err)

	ctx, err := b.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	require.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)
	ctx, err = b.PreAuth(pkg.BackgroundContext(), ctx)
	require.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)

	exp, _ := images.GetStaticImage("color:FF0")
	i, err := b.GenerateTile(pkg.BackgroundContext(), ctx, pkg.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
	require.NoError(t, err)

	assert.Equal(t, *exp, i.Content)
}

func Test_BlendExecute_All(t *testing.T) {
	for _, mode := range allBlendModes {
		b, err := BlendRegistration{}.Initialize(BlendConfig{Mode: mode, Providers: makeBlendProviders()}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
		assert.NotNil(t, b)
		require.NoError(t, err)
		i, err := b.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
		require.NoError(t, err)
		assert.NotNil(t, i)
		assert.Greater(t, len(i.Content), 1000)
	}
}
