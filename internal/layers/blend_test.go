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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/stretchr/testify/assert"
)

var testErrMessages = config.ErrorMessages{}

func makeBlendProviders() []Provider {
	a, _ := ConstructStatic(StaticConfig{Color: "F00"}, nil, &testErrMessages)
	b, _ := ConstructStatic(StaticConfig{Color: "0F0"}, nil, &testErrMessages)

	return []Provider{a, b}
}

func Test_BlendValidate(t *testing.T) {
	b, err := ConstructBlend(BlendConfig{}, nil, &testErrMessages, makeBlendProviders(), nil)
	assert.Nil(t, b)
	assert.Error(t, err)
	b, err = ConstructBlend(BlendConfig{Mode: "fake"}, nil, &testErrMessages, makeBlendProviders(), nil)
	assert.Nil(t, b)
	assert.Error(t, err)
	b, err = ConstructBlend(BlendConfig{Mode: "add", Opacity: 23}, nil, &testErrMessages, makeBlendProviders(), nil)
	assert.Nil(t, b)
	assert.Error(t, err)
	b, err = ConstructBlend(BlendConfig{Mode: "opacity", Opacity: 23}, nil, &testErrMessages, []Provider{}, nil)
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

	b, err := ConstructBlend(BlendConfig{
		Mode: "normal",
		Layer: &BlendLayerConfig{
			Pattern: "something_{a}_{b}",
			Values:  []map[string]string{v1, v2},
		}}, nil, &testErrMessages, makeBlendProviders(), nil)
	assert.NotNil(t, b)
	assert.NoError(t, err)

	assert.Equal(t, 2, len(b.providers))
	assert.Equal(t, &Ref{RefConfig{"something_hello_world"}, nil}, b.providers[0])
	assert.Equal(t, &Ref{RefConfig{"something_goodbye_world"}, nil}, b.providers[1])
}

func Test_BlendExecute_Add(t *testing.T) {
	b, err := ConstructBlend(BlendConfig{Mode: "add"}, nil, &testErrMessages, makeBlendProviders(), nil)
	assert.NotNil(t, b)
	assert.NoError(t, err)

	ctx, err := b.PreAuth(internal.BackgroundContext(), ProviderContext{})
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)
	ctx, err = b.PreAuth(internal.BackgroundContext(), ctx)
	assert.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)

	exp, _ := images.GetStaticImage("color:FF0")
	i, err := b.GenerateTile(internal.BackgroundContext(), ctx, internal.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
	assert.NoError(t, err)

	assert.Equal(t, *exp, *i)
}

func Test_BlendExecute_All(t *testing.T) {
	for _, mode := range allBlendModes {
		b, err := ConstructBlend(BlendConfig{Mode: mode}, nil, &testErrMessages, makeBlendProviders(), nil)
		assert.NotNil(t, b)
		assert.NoError(t, err)
		i, err := b.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Greater(t, len(*i), 1000)
	}
}
