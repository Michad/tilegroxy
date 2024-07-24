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
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
)

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
	b, err := BlendRegistration{}.Initialize(BlendConfig{Providers: providers}, testClientConfig, testErrMessages, nil)
	assert.Nil(t, b)
	assert.Error(t, err)
	b, err = BlendRegistration{}.Initialize(BlendConfig{Mode: "fake", Providers: providers}, testClientConfig, testErrMessages, nil)
	assert.Nil(t, b)
	assert.Error(t, err)
	b, err = BlendRegistration{}.Initialize(BlendConfig{Mode: "add", Opacity: 23, Providers: providers}, testClientConfig, testErrMessages, nil)
	assert.Nil(t, b)
	assert.Error(t, err)
	b, err = BlendRegistration{}.Initialize(BlendConfig{Mode: "opacity", Opacity: 23, Providers: []map[string]interface{}{}}, testClientConfig, testErrMessages, nil)
	assert.Nil(t, b)
	assert.Error(t, err)
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
		}}, testClientConfig, testErrMessages, nil)
	assert.NotNil(t, b)
	assert.NoError(t, err)
	bb := b.(*Blend)

	assert.Len(t, bb.providers, 2)
	assert.Equal(t, &Ref{RefConfig{"something_hello_world"}, nil}, bb.providers[0])
	assert.Equal(t, &Ref{RefConfig{"something_goodbye_world"}, nil}, bb.providers[1])
}

func Test_BlendExecute_Add(t *testing.T) {
	b, err := BlendRegistration{}.Initialize(BlendConfig{Mode: "add", Providers: makeBlendProviders()}, testClientConfig, testErrMessages, nil)
	assert.NotNil(t, b)
	assert.NoError(t, err)

	ctx, err := b.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)
	ctx, err = b.PreAuth(pkg.BackgroundContext(), ctx)
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)

	exp, _ := images.GetStaticImage("color:FF0")
	i, err := b.GenerateTile(pkg.BackgroundContext(), ctx, pkg.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
	assert.NoError(t, err)

	assert.Equal(t, *exp, *i)
}

func Test_BlendExecute_All(t *testing.T) {
	for _, mode := range allBlendModes {
		b, err := BlendRegistration{}.Initialize(BlendConfig{Mode: mode, Providers: makeBlendProviders()}, testClientConfig, testErrMessages, nil)
		assert.NotNil(t, b)
		assert.NoError(t, err)
		i, err := b.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Greater(t, len(*i), 1000)
	}
}
