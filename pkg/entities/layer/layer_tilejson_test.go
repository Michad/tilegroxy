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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var tileJSONErrorMessages = config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}

func Test_ConstructLayer_Examples_OnPlainIDLayer_Fails(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-1", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "tj1",
		Examples: []string{"tj1"},
		Provider: map[string]any{"name": "fixed-tj-1"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, tileJSONErrorMessages, nil, nil, nil)
	require.Error(t, err)
	require.Nil(t, l)
}

func Test_ConstructLayer_Examples_MatchingPattern_Succeeds(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-2", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "tj2",
		Pattern:  "my_{name}_{version}",
		Examples: []string{"my_foo_v1", "my_bar_v2"},
		Provider: map[string]any{"name": "fixed-tj-2"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, tileJSONErrorMessages, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, l)
}

func Test_ConstructLayer_Examples_NotMatchingPattern_Fails(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-3", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "tj3",
		Pattern:  "my_{name}_{version}",
		Examples: []string{"nonmatching"},
		Provider: map[string]any{"name": "fixed-tj-3"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, tileJSONErrorMessages, nil, nil, nil)
	require.Error(t, err)
	require.Nil(t, l)
}

func Test_ConstructLayer_Examples_FailingParamValidator_Fails(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-4", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:             "tj4",
		Pattern:        "my_{name}_{version}",
		ParamValidator: map[string]string{"version": "v[0-9]+"},
		Examples:       []string{"my_foo_bad"},
		Provider:       map[string]any{"name": "fixed-tj-4"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, tileJSONErrorMessages, nil, nil, nil)
	require.Error(t, err)
	require.Nil(t, l)
}

func Test_Layer_IsPattern(t *testing.T) {
	l := &Layer{Config: config.LayerConfig{ID: "plain"}}
	assert.False(t, l.IsPattern())

	l = &Layer{Config: config.LayerConfig{ID: "plain", Pattern: "plain"}}
	assert.False(t, l.IsPattern())

	l = &Layer{Config: config.LayerConfig{ID: "id", Pattern: "my_{name}"}}
	assert.True(t, l.IsPattern())
}

func Test_Layer_TileJSONEligible(t *testing.T) {
	l := &Layer{Config: config.LayerConfig{ID: "plain"}}
	assert.True(t, l.TileJSONEligible())

	l = &Layer{Config: config.LayerConfig{ID: "id", Pattern: "my_{name}"}}
	assert.False(t, l.TileJSONEligible())

	l = &Layer{Config: config.LayerConfig{ID: "id", Pattern: "my_{name}", Examples: []string{"my_foo"}}}
	assert.True(t, l.TileJSONEligible())
}

func Test_Layer_TileJSONNames(t *testing.T) {
	l := &Layer{ID: "plain", Config: config.LayerConfig{ID: "plain"}}
	assert.Equal(t, []string{"plain"}, l.TileJSONNames())

	l = &Layer{ID: "id", Config: config.LayerConfig{ID: "id", Pattern: "my_{name}", Examples: []string{"my_foo", "my_bar"}}}
	assert.Equal(t, []string{"my_foo", "my_bar"}, l.TileJSONNames())
}

func Test_Layer_BuildTileJSON_Defaults(t *testing.T) {
	l := &Layer{ID: "l1", Config: config.LayerConfig{ID: "l1"}, DataType: config.DataTypeUnknown}

	doc := l.BuildTileJSON("l1", []string{"https://example.com/tiles/l1/{z}/{x}/{y}"}, nil)

	assert.Equal(t, "3.0.0", doc.TileJSON)
	assert.Equal(t, "l1", doc.Name)
	assert.Equal(t, []string{"https://example.com/tiles/l1/{z}/{x}/{y}"}, doc.Tiles)
	assert.Equal(t, 0, doc.MinZoom)
	assert.Equal(t, pkg.MaxZoom, doc.MaxZoom)
	assert.Empty(t, doc.Description)
	assert.Empty(t, doc.Attribution)
	require.Len(t, doc.Bounds, 4)

	world := pkg.WorldBounds()
	assert.InDelta(t, world.West, doc.Bounds[0], 0.0001)
	assert.InDelta(t, world.South, doc.Bounds[1], 0.0001)
	assert.InDelta(t, world.East, doc.Bounds[2], 0.0001)
	assert.InDelta(t, world.North, doc.Bounds[3], 0.0001)
}

func Test_Layer_BuildTileJSON_ExplicitFields(t *testing.T) {
	minZoom := 4
	maxZoom := 16
	l := &Layer{
		ID: "l2",
		Config: config.LayerConfig{
			ID:          "l2",
			MinZoom:     &minZoom,
			MaxZoom:     &maxZoom,
			Description: "Aerial imagery",
			Attribution: "(c) Example",
			Bounds:      config.BoundsConfig{South: 51, North: 63, West: -7, East: 0.1},
		},
		DataType: config.DataTypeRaster,
	}

	doc := l.BuildTileJSON("l2", []string{"https://example.com/tiles/l2/{z}/{x}/{y}"}, nil)

	assert.Equal(t, 4, doc.MinZoom)
	assert.Equal(t, 16, doc.MaxZoom)
	assert.Equal(t, "Aerial imagery", doc.Description)
	assert.Equal(t, "(c) Example", doc.Attribution)
	assert.Equal(t, []float64{-7, 51, 0.1, 63}, doc.Bounds)
}

func Test_Layer_BuildTileJSON_IntersectsAllowedArea(t *testing.T) {
	l := &Layer{
		ID: "l4",
		Config: config.LayerConfig{
			ID:     "l4",
			Bounds: config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		},
		DataType: config.DataTypeRaster,
	}

	allowed := pkg.Bounds{South: -5, North: 5, West: -5, East: 20, SRID: pkg.SRIDWGS84}

	doc := l.BuildTileJSON("l4", []string{"https://example.com/tiles/l4/{z}/{x}/{y}"}, &allowed)

	assert.Equal(t, []float64{-5, -5, 10, 5}, doc.Bounds)
}
