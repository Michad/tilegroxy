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

package layers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var tileJSONErrorMessages = config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}

func Test_ConstructLayer_Examples_OnPlainIDLayer_Fails(t *testing.T) {
	layer.RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-1", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "tj1",
		Examples: []string{"tj1"},
		Provider: map[string]any{"name": "fixed-tj-1"},
	}

	l, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, tileJSONErrorMessages, nil, nil, nil)
	require.Error(t, err)
	require.Nil(t, l)
}

func Test_ConstructLayer_Examples_MatchingPattern_Succeeds(t *testing.T) {
	layer.RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-2", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "tj2",
		Pattern:  "my_{name}_{version}",
		Examples: []string{"my_foo_v1", "my_bar_v2"},
		Provider: map[string]any{"name": "fixed-tj-2"},
	}

	l, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, tileJSONErrorMessages, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, l)
}

func Test_ConstructLayer_Examples_NotMatchingPattern_Fails(t *testing.T) {
	layer.RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-3", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "tj3",
		Pattern:  "my_{name}_{version}",
		Examples: []string{"nonmatching"},
		Provider: map[string]any{"name": "fixed-tj-3"},
	}

	l, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, tileJSONErrorMessages, nil, nil, nil)
	require.Error(t, err)
	require.Nil(t, l)
}

func Test_ConstructLayer_Examples_FailingParamValidator_Fails(t *testing.T) {
	layer.RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-4", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:             "tj4",
		Pattern:        "my_{name}_{version}",
		ParamValidator: map[string]string{"version": "v[0-9]+"},
		Examples:       []string{"my_foo_bad"},
		Provider:       map[string]any{"name": "fixed-tj-4"},
	}

	l, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, tileJSONErrorMessages, nil, nil, nil)
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
	assert.Empty(t, doc.Encoding)
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
	l := resolvedLayer(t, config.LayerConfig{
		ID:            "l2",
		LayerMetadata: config.LayerMetadata{MinZoom: &minZoom, MaxZoom: &maxZoom, Bounds: config.BoundsConfig{South: 51, North: 63, West: -7, East: 0.1}, TileJSONMetadata: config.TileJSONMetadata{Description: "Aerial imagery", Attribution: "(c) Example"}},
	}, layer.Description{})

	doc := l.BuildTileJSON("l2", []string{"https://example.com/tiles/l2/{z}/{x}/{y}"}, nil)

	assert.Equal(t, 4, doc.MinZoom)
	assert.Equal(t, 16, doc.MaxZoom)
	assert.Equal(t, "Aerial imagery", doc.Description)
	assert.Equal(t, "(c) Example", doc.Attribution)
	assert.Equal(t, []float64{-7, 51, 0.1, 63}, doc.Bounds)
}

// MapLibre GL JS reads encoding from TileJSON, so an MLT source configures itself from the document.
func Test_Layer_BuildTileJSON_MltEncoding(t *testing.T) {
	l := resolvedLayer(t, config.LayerConfig{ID: "l3", LayerMetadata: config.LayerMetadata{DataType: config.DataTypeMLT}}, layer.Description{})

	doc := l.BuildTileJSON("l3", []string{"https://example.com/tiles/l3/{z}/{x}/{y}"}, nil)
	assert.Equal(t, "mlt", doc.Encoding)

	l = resolvedLayer(t, config.LayerConfig{ID: "l3", LayerMetadata: config.LayerMetadata{DataType: config.DataTypeMVT}}, layer.Description{})

	doc = l.BuildTileJSON("l3", []string{"https://example.com/tiles/l3/{z}/{x}/{y}"}, nil)
	assert.Empty(t, doc.Encoding)
}

func Test_Layer_BuildTileJSON_IntersectsAllowedArea(t *testing.T) {
	l := resolvedLayer(t, config.LayerConfig{
		ID:            "l4",
		LayerMetadata: config.LayerMetadata{Bounds: config.BoundsConfig{South: -10, North: 10, West: -10, East: 10}},
	}, layer.Description{})

	allowed := pkg.Bounds{South: -5, North: 5, West: -5, East: 20, SRID: pkg.SRIDWGS84}

	doc := l.BuildTileJSON("l4", []string{"https://example.com/tiles/l4/{z}/{x}/{y}"}, &allowed)

	assert.Equal(t, []float64{-5, -5, 10, 5}, doc.Bounds)
}

func Test_ConstructLayer_EmptyParamValidator_ErrorsNotPanics(t *testing.T) {
	layer.RegisterProvider(fixedTypeTestRegistration{name: "fixed-tj-empty", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:             "tj-empty",
		Pattern:        "tile_{name}",
		ParamValidator: map[string]string{"name": ""},
		Provider:       map[string]any{"name": "fixed-tj-empty"},
	}

	require.NotPanics(t, func() {
		l, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, tileJSONErrorMessages, nil, nil, nil)
		require.Error(t, err)
		require.Nil(t, l)
		assert.Contains(t, err.Error(), "layer.paramValidator.name")
	})
}

func testDescription() layer.Description {
	minZoom, maxZoom := 3, 9
	return layer.Description{
		MinZoom:          &minZoom,
		MaxZoom:          &maxZoom,
		Bounds:           config.BoundsConfig{South: 10, North: 20, West: 30, East: 40},
		TileJSONMetadata: config.TileJSONMetadata{Description: "archive description", Attribution: "archive attribution", Version: "1.2", Center: []float64{35, 15, 5}, VectorLayers: []config.VectorLayer{{ID: "roads"}}},
	}
}

func resolvedLayer(t *testing.T, cfg config.LayerConfig, reported layer.Description) *Layer {
	t.Helper()

	md, err := resolveMetadata(cfg.ID, cfg.LayerMetadata, reported, tileJSONErrorMessages)
	require.NoError(t, err)

	return &Layer{ID: cfg.ID, Config: cfg, metadata: md}
}

func Test_Layer_BuildTileJSON_FromMetadata(t *testing.T) {
	l := resolvedLayer(t, config.LayerConfig{ID: "m"}, testDescription())

	doc := l.BuildTileJSON("m", []string{"https://example.com/tiles/m/{z}/{x}/{y}"}, nil)

	assert.Equal(t, 3, doc.MinZoom)
	assert.Equal(t, 9, doc.MaxZoom)
	assert.Equal(t, []float64{30, 10, 40, 20}, doc.Bounds)
	assert.Equal(t, "archive description", doc.Description)
	assert.Equal(t, "archive attribution", doc.Attribution)
	assert.Equal(t, "1.2", doc.Version)
	assert.Equal(t, []float64{35, 15, 5}, doc.Center)
	assert.Equal(t, []config.VectorLayer{{ID: "roads"}}, doc.VectorLayers)
	assert.Equal(t, "m", doc.Name)
}

func Test_Layer_BuildTileJSON_ConfigOverridesMetadata(t *testing.T) {
	minZoom, maxZoom := 1, 15
	l := resolvedLayer(t, config.LayerConfig{
		ID:            "m",
		LayerMetadata: config.LayerMetadata{MinZoom: &minZoom, MaxZoom: &maxZoom, Bounds: config.BoundsConfig{South: 11, North: 12, West: 31, East: 32}, TileJSONMetadata: config.TileJSONMetadata{Description: "layer description", Attribution: "layer attribution"}},
	}, testDescription())

	doc := l.BuildTileJSON("m", nil, nil)

	assert.Equal(t, 1, doc.MinZoom)
	assert.Equal(t, 15, doc.MaxZoom)
	assert.Equal(t, []float64{31, 11, 32, 12}, doc.Bounds)
	assert.Equal(t, "layer description", doc.Description)
	assert.Equal(t, "layer attribution", doc.Attribution)
	assert.Equal(t, "1.2", doc.Version)
	assert.Nil(t, doc.Center, "the provider's center lies outside the layer's bounds")
}

func Test_Layer_BuildTileJSON_MetadataBoundsIntersectAllowedArea(t *testing.T) {
	l := resolvedLayer(t, config.LayerConfig{ID: "m"}, testDescription())

	doc := l.BuildTileJSON("m", nil, &pkg.Bounds{South: 15, North: 25, West: 35, East: 45, SRID: pkg.SRIDWGS84})

	assert.Equal(t, []float64{35, 15, 40, 20}, doc.Bounds)
	assert.Equal(t, []float64{35, 15, 5}, doc.Center)
}

func Test_Layer_BuildTileJSON_AllowedAreaExcludingCenterDropsIt(t *testing.T) {
	l := resolvedLayer(t, config.LayerConfig{ID: "m"}, testDescription())

	doc := l.BuildTileJSON("m", nil, &pkg.Bounds{South: 18, North: 25, West: 38, East: 45, SRID: pkg.SRIDWGS84})

	assert.Equal(t, []float64{38, 18, 40, 20}, doc.Bounds)
	assert.Nil(t, doc.Center)
}

func Test_Layer_BuildTileJSON_OmitsEmptyMetadataFields(t *testing.T) {
	l := &Layer{ID: "l", Config: config.LayerConfig{ID: "l"}}

	b, err := json.Marshal(l.BuildTileJSON("l", nil, nil))
	require.NoError(t, err)

	assert.NotContains(t, string(b), "version")
	assert.NotContains(t, string(b), "center")
	assert.NotContains(t, string(b), "vector_layers")
}

func Test_VectorLayer_MarshalMatchesSpec(t *testing.T) {
	minZoom := 0
	b, err := json.Marshal([]config.VectorLayer{
		{ID: "roads"},
		{ID: "water", Fields: map[string]string{"name": "String"}, Description: "lakes", MinZoom: &minZoom},
	})
	require.NoError(t, err)

	assert.JSONEq(t, `[{"id":"roads","fields":{}},{"id":"water","fields":{"name":"String"},"description":"lakes","minzoom":0}]`, string(b))
}

func Test_Layer_BuildTileJSON_ConfigOnlyMetadataFields(t *testing.T) {
	l := resolvedLayer(t, config.LayerConfig{
		ID: "m",
		LayerMetadata: config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{
			Version:      "2.0",
			Center:       []float64{31, 12, 4},
			VectorLayers: []config.VectorLayer{{ID: "water"}},
		}},
	}, testDescription())

	doc := l.BuildTileJSON("m", nil, nil)

	assert.Equal(t, "2.0", doc.Version)
	assert.Equal(t, []float64{31, 12, 4}, doc.Center)
	assert.Equal(t, []config.VectorLayer{{ID: "water"}}, doc.VectorLayers)
	assert.Equal(t, "archive description", doc.Description)
}
