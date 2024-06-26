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
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/stretchr/testify/assert"
)

func makeEffectProvider() Provider {
	p, _ := ConstructStatic(StaticConfig{Color: "F00"}, nil, &testErrMessages)
	return *p
}

func Test_EffectValidate(t *testing.T) {
	s := makeEffectProvider()
	c, err := ConstructEffect(EffectConfig{}, nil, &testErrMessages, &s)

	assert.Nil(t, c)
	assert.Error(t, err)

	c, err = ConstructEffect(EffectConfig{Mode: "emboss", Intensity: 24}, nil, &testErrMessages, &s)

	assert.Nil(t, c)
	assert.Error(t, err)
}

func Test_EffectExecuteGreyscale(t *testing.T) {
	s := makeEffectProvider()
	c, err := ConstructEffect(EffectConfig{Mode: "grayscale"}, nil, &testErrMessages, &s)

	assert.NotNil(t, c)
	assert.NoError(t, err)

	pc, err := c.PreAuth(internal.BackgroundContext(), ProviderContext{})
	assert.NotNil(t, pc)
	assert.NoError(t, err)

	exp, _ := images.GetStaticImage("color:4d4d4d")
	img, err := c.GenerateTile(internal.BackgroundContext(), pc, internal.TileRequest{LayerName: "l", Z: 5, X: 3, Y: 1})
	assert.NotNil(t, img)
	assert.NoError(t, err)

	assert.Equal(t, *exp, *img)
}

func Test_EffectExecuteAll(t *testing.T) {
	s := makeEffectProvider()
	for _, mode := range allEffectModes {
		c, err := ConstructEffect(EffectConfig{Mode: mode}, nil, &testErrMessages, &s)

		assert.NotNil(t, c)
		assert.NoError(t, err)

		pc, err := c.PreAuth(internal.BackgroundContext(), ProviderContext{})
		assert.NotNil(t, pc)
		assert.NoError(t, err)

		img, err := c.GenerateTile(internal.BackgroundContext(), pc, internal.TileRequest{LayerName: "l", Z: 5, X: 3, Y: 1})
		assert.NotNil(t, img)
		assert.NoError(t, err)
	}
}
