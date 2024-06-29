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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/stretchr/testify/assert"
)

var testErrMessages = config.ErrorMessages{}

func makeBlendProviders() []*Provider {
	a, _ := ConstructStatic(StaticConfig{Color: "F00"}, nil, &testErrMessages)
	b, _ := ConstructStatic(StaticConfig{Color: "0F0"}, nil, &testErrMessages)
	var ap Provider = *a
	var bp Provider = *b

	return []*Provider{&ap, &bp}
}

func Test_BlendValidate(t *testing.T) {
	b, err := ConstructBlend(BlendConfig{}, nil, &testErrMessages, makeBlendProviders())
	assert.Nil(t, b)
	assert.NotNil(t, err)
	b, err = ConstructBlend(BlendConfig{Mode: "fake"}, nil, &testErrMessages, makeBlendProviders())
	assert.Nil(t, b)
	assert.NotNil(t, err)
	b, err = ConstructBlend(BlendConfig{Mode: "add", Opacity: 23}, nil, &testErrMessages, makeBlendProviders())
	assert.Nil(t, b)
	assert.NotNil(t, err)
	b, err = ConstructBlend(BlendConfig{Mode: "opacity", Opacity: 23}, nil, &testErrMessages, []*Provider{})
	assert.Nil(t, b)
	assert.NotNil(t, err)
}

func Test_BlendExecute_Add(t *testing.T) {
	b, err := ConstructBlend(BlendConfig{Mode: "add"}, nil, &testErrMessages, makeBlendProviders())
	assert.NotNil(t, b)
	assert.Nil(t, err)

	ctx, err := b.PreAuth(internal.BackgroundContext(), ProviderContext{})
	assert.Nil(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)
	ctx, err = b.PreAuth(internal.BackgroundContext(), ctx)
	assert.Nil(t, err)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Other)

	exp, _ := images.GetStaticImage("color:FF0")
	i, err := b.GenerateTile(internal.BackgroundContext(), ctx, internal.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
	assert.Nil(t, err)

	assert.Equal(t, *exp, *i)
}

func Test_BlendExecute_All(t *testing.T) {
	for _, mode := range allBlendModes {
		b, err := ConstructBlend(BlendConfig{Mode: mode}, nil, &testErrMessages, makeBlendProviders())
		assert.NotNil(t, b)
		assert.Nil(t, err)
		i, err := b.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "", Z: 4, X: 2, Y: 3})
		assert.Nil(t, err)
		assert.NotNil(t, i)
		assert.Greater(t, len(*i), 1000)
	}
}
