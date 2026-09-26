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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func zoomPtr(z int) *int { return &z }

func Test_MergeMetadata_UnionsChildren(t *testing.T) {
	a := config.LayerMetadata{
		DataType:         config.DataTypeMVT,
		MinZoom:          zoomPtr(2),
		MaxZoom:          zoomPtr(10),
		Bounds:           config.BoundsConfig{South: 0, North: 10, West: 0, East: 10},
		TileJSONMetadata: config.TileJSONMetadata{Description: "a", Attribution: "OSM", Center: []float64{1, 2, 3}, VectorLayers: []config.VectorLayer{{ID: "roads"}, {ID: "water"}}},
	}
	b := config.LayerMetadata{
		DataType:         config.DataTypeRaster,
		MinZoom:          zoomPtr(0),
		MaxZoom:          zoomPtr(14),
		Bounds:           config.BoundsConfig{South: -5, North: 5, West: -5, East: 5},
		TileJSONMetadata: config.TileJSONMetadata{Description: "b", Version: "2", Attribution: "Overture", VectorLayers: []config.VectorLayer{{ID: "water", Fields: map[string]string{"kind": "String"}}, {ID: "buildings"}}},
	}

	md := mergeMetadata(metadataOnlyProvider{md: a}, metadataOnlyProvider{md: b}, metadataOnlyProvider{md: config.LayerMetadata{MinZoom: zoomPtr(1), MaxZoom: zoomPtr(1), Bounds: config.BoundsConfig{South: 1, North: 2, West: 1, East: 2}, TileJSONMetadata: config.TileJSONMetadata{Attribution: "OSM"}}})

	assert.Equal(t, config.DataTypeMVT, md.DataType)
	assert.Equal(t, 0, *md.MinZoom)
	assert.Equal(t, 14, *md.MaxZoom)
	assert.Equal(t, config.BoundsConfig{South: -5, North: 10, West: -5, East: 10}, md.Bounds)
	assert.Equal(t, "a", md.Description)
	assert.Equal(t, "2", md.Version)
	assert.Equal(t, "OSM, Overture", md.Attribution)
	assert.Equal(t, []float64{1, 2, 3}, md.Center)
	assert.Equal(t, []config.VectorLayer{{ID: "roads"}, {ID: "water"}, {ID: "buildings"}}, md.VectorLayers)
}

func Test_MergeMetadata_UnknownLimitsStayUnset(t *testing.T) {
	a := config.LayerMetadata{MinZoom: zoomPtr(2), MaxZoom: zoomPtr(10), Bounds: config.BoundsConfig{North: 1, East: 1}}

	md := mergeMetadata(metadataOnlyProvider{md: a}, &closableProvider{})

	assert.Nil(t, md.MinZoom)
	assert.Nil(t, md.MaxZoom)
	assert.Zero(t, md.Bounds)
	assert.Nil(t, md.VectorLayers)
	assert.Empty(t, md.Attribution)
}

func Test_MergeMetadata_SingleChildPassesThrough(t *testing.T) {
	a := config.LayerMetadata{MinZoom: zoomPtr(2), MaxZoom: zoomPtr(10), Bounds: config.BoundsConfig{North: 1, East: 1}, TileJSONMetadata: config.TileJSONMetadata{VectorLayers: []config.VectorLayer{{ID: "roads"}}}}

	md := mergeMetadata(metadataOnlyProvider{md: a})

	assert.Equal(t, a.MinZoom, md.MinZoom)
	assert.Equal(t, a.MaxZoom, md.MaxZoom)
	assert.Equal(t, a.Bounds, md.Bounds)
	assert.Equal(t, []config.VectorLayer{{ID: "roads"}}, md.VectorLayers)
}

func Test_Fallback_PicksFirstMetadata(t *testing.T) {
	primary := config.LayerMetadata{DataType: config.DataTypeMVT, MinZoom: zoomPtr(4), MaxZoom: zoomPtr(8), TileJSONMetadata: config.TileJSONMetadata{Description: "primary"}}
	secondary := config.LayerMetadata{MinZoom: zoomPtr(0), MaxZoom: zoomPtr(6), TileJSONMetadata: config.TileJSONMetadata{Description: "secondary"}}

	md := Fallback{Primary: metadataOnlyProvider{md: primary}, Secondary: metadataOnlyProvider{md: secondary}}.Metadata()

	assert.Equal(t, config.DataTypeMVT, md.DataType)
	assert.Equal(t, "primary", md.Description)
	require.NotNil(t, md.MinZoom)
	assert.Equal(t, 4, *md.MinZoom)
	assert.Equal(t, 8, *md.MaxZoom)
}

func Test_CompositeMVT_MergesChildMetadata(t *testing.T) {
	c := CompositeMVT{providers: []layer.Provider{
		layer.ProviderWrapper{Name: "a", Provider: metadataOnlyProvider{md: config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{VectorLayers: []config.VectorLayer{{ID: "roads"}}}}}},
		metadataOnlyProvider{md: config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{VectorLayers: []config.VectorLayer{{ID: "water"}}}}},
	}}

	md := c.Metadata()

	assert.Equal(t, config.DataTypeMVT, md.DataType)
	assert.Equal(t, []config.VectorLayer{{ID: "roads"}, {ID: "water"}}, md.VectorLayers)
}

func Test_CropMvt_IntersectsPrimaryBounds(t *testing.T) {
	crop := pkg.Bounds{South: 0, North: 10, West: 0, East: 10}
	primary := metadataOnlyProvider{md: config.LayerMetadata{Bounds: config.BoundsConfig{South: -5, North: 5, West: -5, East: 5}}}

	md := CropMvt{CropMvtConfig: CropMvtConfig{Bounds: crop}, Primary: primary}.Metadata()
	assert.Equal(t, config.BoundsConfig{South: 0, North: 5, West: 0, East: 5}, md.Bounds)

	md = CropMvt{CropMvtConfig: CropMvtConfig{Bounds: crop}, Primary: metadataOnlyProvider{}}.Metadata()
	assert.Equal(t, crop.ToConfig(), md.Bounds)

	md = CropMvt{CropMvtConfig: CropMvtConfig{Bounds: crop, BoundsFromAuth: true}, Primary: primary}.Metadata()
	assert.Equal(t, primary.md.Bounds, md.Bounds)
}
