// Copyright 2026 Michael Davis
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

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

func Test_ConstructLayer_Bounds_Raster_WrapsInRealCrop(t *testing.T) {
	rawConfig := config.LayerConfig{
		ID:     "real-crop-layer",
		Bounds: config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{
			"name":  "static",
			"color": "F00",
		},
	}

	l, err := layer.ConstructLayer(rawConfig, config.ClientConfig{}, testErrMessages, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
	wrapper, ok := l.Provider.(layer.ProviderWrapper)
	require.True(t, ok)
	require.Equal(t, "crop", wrapper.Name)
}

func Test_ConstructLayer_Bounds_MVT_WrapsInRealCropMvt(t *testing.T) {
	rawConfig := config.LayerConfig{
		ID:     "real-cropmvt-layer",
		Bounds: config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{
			"name":      "compositemvt",
			"providers": []map[string]interface{}{},
		},
	}

	l, err := layer.ConstructLayer(rawConfig, config.ClientConfig{}, testErrMessages, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
	wrapper, ok := l.Provider.(layer.ProviderWrapper)
	require.True(t, ok)
	require.Equal(t, "cropmvt", wrapper.Name)
}
