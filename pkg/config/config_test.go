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

package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Proxying vector tiles is a documented use case, so the default allowlist must cover MVT and MLT
func TestDefaultConfig_ContentTypesIncludesVectorTileTypes(t *testing.T) {
	c := DefaultConfig()

	assert.Contains(t, c.Client.ContentTypes, "application/vnd.mapbox-vector-tile")
	assert.Contains(t, c.Client.ContentTypes, "application/x-protobuf")
	assert.Contains(t, c.Client.ContentTypes, "application/vnd.maplibre-tile")
	assert.Contains(t, c.Client.ContentTypes, "application/vnd.maplibre-vector-tile")
}

func Test_DrainDelayDefaultsToFive(t *testing.T) {
	c := DefaultConfig()

	assert.Equal(t, uint(5), c.Server.DrainDelay)
}

func Test_LayerConfig_HasDataTypeZoomBoundsFields(t *testing.T) {
	minZoom := 4
	maxZoom := 18
	cfg := LayerConfig{
		LayerMetadata: LayerMetadata{DataType: DataTypeRaster, MinZoom: &minZoom, MaxZoom: &maxZoom, Bounds: BoundsConfig{South: -10, North: 10, West: -10, East: 10}},
	}

	assert.Equal(t, DataTypeRaster, cfg.DataType)
	assert.Equal(t, 4, *cfg.MinZoom)
	assert.Equal(t, 18, *cfg.MaxZoom)
	assert.InDelta(t, 10.0, cfg.Bounds.North, 0)
}

func Test_DataType_Constants_Values(t *testing.T) {
	assert.Equal(t, DataTypeRaster, DataType("raster"))
	assert.Equal(t, DataTypeMVT, DataType("mvt"))
	assert.Equal(t, DataTypeUnknown, DataType("unknown"))
}

func Test_VectorLayer_MarshalJSON_NilFieldsIsObject(t *testing.T) {
	b, err := json.Marshal(VectorLayer{ID: "roads"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"roads","fields":{}}`, string(b))
}
