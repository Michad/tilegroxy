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

package layer

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

func Test_RenderTileNoCache_BelowMinZoom_ReturnsRangeError(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-zoom-1", dt: config.DataTypeRaster})

	minZoom := 4
	rawConfig := config.LayerConfig{
		ID:       "z1",
		MinZoom:  &minZoom,
		Provider: map[string]any{"name": "fixed-zoom-1"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)
	require.NoError(t, err)

	_, err = l.RenderTileNoCache(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "z1", Z: 2, X: 0, Y: 0})

	require.Error(t, err)
	var rangeErr pkg.RangeError
	require.ErrorAs(t, err, &rangeErr)
}

func Test_RenderTileNoCache_AboveMaxZoom_ReturnsRangeError(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-zoom-2", dt: config.DataTypeRaster})

	maxZoom := 10
	rawConfig := config.LayerConfig{
		ID:       "z2",
		MaxZoom:  &maxZoom,
		Provider: map[string]any{"name": "fixed-zoom-2"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)
	require.NoError(t, err)

	_, err = l.RenderTileNoCache(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "z2", Z: 15, X: 0, Y: 0})

	require.Error(t, err)
	var rangeErr pkg.RangeError
	require.ErrorAs(t, err, &rangeErr)
}

func Test_RenderTileNoCache_WithinZoomRange_Succeeds(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-zoom-3", dt: config.DataTypeRaster})

	minZoom := 4
	maxZoom := 10
	rawConfig := config.LayerConfig{
		ID:       "z3",
		MinZoom:  &minZoom,
		MaxZoom:  &maxZoom,
		Provider: map[string]any{"name": "fixed-zoom-3"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)
	require.NoError(t, err)

	_, err = l.RenderTileNoCache(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "z3", Z: 7, X: 0, Y: 0})

	require.NoError(t, err)
}

func Test_RenderTileNoCache_NoZoomLimitsConfigured_Succeeds(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-zoom-4", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "z4",
		Provider: map[string]any{"name": "fixed-zoom-4"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)
	require.NoError(t, err)

	_, err = l.RenderTileNoCache(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "z4", Z: 20, X: 0, Y: 0})

	require.NoError(t, err)
}

func Test_CheckZoomBounds_BothLimitsConfigured_ReportsConfiguredRangeNotGlobalDefaults(t *testing.T) {
	minZoom := 4
	maxZoom := 10
	l := &Layer{Config: config.LayerConfig{ID: "z5", MinZoom: &minZoom, MaxZoom: &maxZoom}}

	err := l.CheckZoomBounds(pkg.TileRequest{LayerName: "z5", Z: 2, X: 0, Y: 0})
	require.Error(t, err)
	var rangeErr pkg.RangeError
	require.ErrorAs(t, err, &rangeErr)
	require.InDelta(t, float64(minZoom), rangeErr.MinValue, 0)
	require.InDelta(t, float64(maxZoom), rangeErr.MaxValue, 0)

	err = l.CheckZoomBounds(pkg.TileRequest{LayerName: "z5", Z: 15, X: 0, Y: 0})
	require.Error(t, err)
	require.ErrorAs(t, err, &rangeErr)
	require.InDelta(t, float64(minZoom), rangeErr.MinValue, 0)
	require.InDelta(t, float64(maxZoom), rangeErr.MaxValue, 0)
}
