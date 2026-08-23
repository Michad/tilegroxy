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
	require.Equal(t, pkg.DataTypeRaster, l.Provider.DataType())
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
	require.Equal(t, pkg.DataTypeMVT, l.Provider.DataType())
}

// A layer.Provider that owns a resource (here, a Custom provider's Yaegi interpreter and close
// hook) must not be silently orphaned when bounds triggers crop wrapping. resolveProviderWithBounds
// builds the primary provider once to read its DataType(), then again inside the crop wrapper
// (crop/cropmvt only accept raw config, not an already-built Provider); the first instance must be
// closed, not discarded, or a Custom provider's close hook - and whatever cleanup it does - never
// runs. Custom itself reports DataTypeUnknown (Task 3), so datatype must be set explicitly here
// for bounds wrapping to resolve at all.
func Test_ConstructLayer_Bounds_ClosesDiscardedCustomProvider(t *testing.T) {
	out := filepath.Join(t.TempDir(), "closed.txt")

	script := `
package custom

import (
	"context"
	"os"

	"tilegroxy/tilegroxy"
)

func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages) (tilegroxy.ProviderContext, error) {
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages) (*tilegroxy.Image, error) {
	return &tilegroxy.Image{Content: []byte{0x01, 0x02}}, nil
}

func close(ctx context.Context) error {
	return os.WriteFile("` + out + `", []byte("closed"), 0600)
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

	_, statErr := os.Stat(out)
	require.NoError(t, statErr, "the discarded pre-wrap Custom provider's close hook must have run during layer construction")
}
