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

func Test_DataType_Effect(t *testing.T) {
	assert.Equal(t, config.DataTypeRaster, EffectRegistration{}.DataType(EffectConfig{}))
}

func makeEffectProvider() map[string]interface{} {
	return map[string]interface{}{
		"name":  "static",
		"color": "F00",
	}
}

func Test_EffectValidate(t *testing.T) {
	s := makeEffectProvider()
	c, err := EffectRegistration{}.Initialize(EffectConfig{Provider: s}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, c)
	require.Error(t, err)

	c, err = EffectRegistration{}.Initialize(EffectConfig{Mode: "emboss", Intensity: 24, Provider: s}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, c)
	require.Error(t, err)
}

func Test_EffectValidateIntensityRange(t *testing.T) {
	s := makeEffectProvider()

	invalid := map[string][]float64{
		"gaussian":   {-1, maxRadius + 1, 1e9},
		"blur":       {-1, maxRadius + 1},
		"median":     {maxRadius + 1},
		"brightness": {-1.5, 1.5},
		"saturation": {2},
		"hue":        {-361, 361},
		"gamma":      {-1, maxGamma + 1},
		"threshold":  {-1, 256},
	}

	for mode, intensities := range invalid {
		for _, intensity := range intensities {
			c, err := EffectRegistration{}.Initialize(EffectConfig{Mode: mode, Intensity: intensity, Provider: s}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

			assert.Nil(t, c, "%v %v", mode, intensity)
			require.Error(t, err, "%v %v", mode, intensity)
		}
	}

	valid := map[string][]float64{
		"gaussian":   {0, 1, maxRadius},
		"brightness": {-1, 0.5, 1},
		"hue":        {-360, 360},
		"gamma":      {0, maxGamma},
		"threshold":  {0, 128, 255},
	}

	for mode, intensities := range valid {
		for _, intensity := range intensities {
			c, err := EffectRegistration{}.Initialize(EffectConfig{Mode: mode, Intensity: intensity, Provider: s}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

			assert.NotNil(t, c, "%v %v", mode, intensity)
			require.NoError(t, err, "%v %v", mode, intensity)
		}
	}
}

func Test_EffectExecuteGreyscale(t *testing.T) {
	s := makeEffectProvider()
	c, err := EffectRegistration{}.Initialize(EffectConfig{Mode: "grayscale", Provider: s}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.NotNil(t, c)
	require.NoError(t, err)

	pc, err := c.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	exp, _ := images.GetStaticImage("color:4d4d4d")
	img, err := c.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 5, X: 3, Y: 1})
	assert.NotNil(t, img)
	require.NoError(t, err)

	assert.Equal(t, *exp, img.Content)
}

func Test_EffectExecuteAll(t *testing.T) {
	s := makeEffectProvider()
	for _, mode := range allEffectModes {
		c, err := EffectRegistration{}.Initialize(EffectConfig{Mode: mode, Provider: s}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

		assert.NotNil(t, c)
		require.NoError(t, err)

		pc, err := c.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
		assert.NotNil(t, pc)
		require.NoError(t, err)

		img, err := c.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 5, X: 3, Y: 1})
		assert.NotNil(t, img)
		require.NoError(t, err)
	}
}
