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
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

// This package's init() functions register the real crop/cropmvt providers (unlike
// pkg/entities/layer's own tests, which use stand-ins to avoid a circular import), so this is
// where the real wiring between layer.ConstructLayer and the crop/cropmvt providers gets covered.
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

// Data type is now resolved from the primary's raw config alone (ProviderRegistration.DataType),
// without constructing anything. So when bounds triggers crop wrapping, a Custom provider's Yaegi
// interpreter must be built exactly once, inside the crop wrapper - never as a discarded, unwrapped
// instance beforehand. The script increments a counter file on each construction so a regression
// that reintroduces a pre-wrap build shows up as a count greater than one.
func Test_ConstructLayer_Bounds_ConstructsCustomProviderOnce(t *testing.T) {
	out := filepath.Join(t.TempDir(), "construct-count.txt")

	script := `
package custom

import (
	"os"

	"tilegroxy/tilegroxy"
)

func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages) (tilegroxy.ProviderContext, error) {
	f, _ := os.OpenFile("` + out + `", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	f.WriteString("x")
	f.Close()
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages) (*tilegroxy.Image, error) {
	return &tilegroxy.Image{Content: []byte{0x01, 0x02}}, nil
}
`

	rawConfig := config.LayerConfig{
		ID:       "real-crop-custom-layer",
		DataType: pkg.DataTypeRaster,
		Bounds:   config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{
			"name":   "custom",
			"script": script,
		},
	}

	l, err := layer.ConstructLayer(rawConfig, config.ClientConfig{}, testErrMessages, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)

	_, err = l.Provider.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	require.NoError(t, err)

	content, statErr := os.ReadFile(out)
	require.NoError(t, statErr)
	require.Len(t, content, 1, "PreAuth on the wrapped provider must reach exactly one underlying Custom instance")
}
