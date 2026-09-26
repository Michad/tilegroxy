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
	"cmp"
	"encoding/json"
)

// Describes a layer's tiles. Set on the layer by the operator or reported by its provider.
type LayerMetadata struct {
	DataType         DataType     // Optional. Declares this layer's data type. Must not contradict the provider's own DataType(); required if Bounds is set and the provider's type is unknown
	MinZoom          *int         // Optional. Requests below this zoom are rejected as out of bounds. nil means no lower limit
	MaxZoom          *int         // Optional. Requests above this zoom are rejected as out of bounds. nil means no upper limit
	Bounds           BoundsConfig // Optional. Automatically wraps this layer's provider in crop/cropmvt, restricting it to this geographic area
	TileJSONMetadata `mapstructure:",squash" yaml:",inline"`
}

// Fields copied as-is into a layer's TileJSON document. Have no effect unless TileJSON is enabled.
type TileJSONMetadata struct {
	Description  string        `json:"description,omitempty"`   // Optional. Populates the `description` field
	Attribution  string        `json:"attribution,omitempty"`   // Optional. Populates the `attribution` field
	Version      string        `json:"version,omitempty"`       // Optional. Populates the `version` field
	Center       []float64     `json:"center,omitempty"`        // Optional. Populates the `center` field as longitude, latitude, zoom
	VectorLayers []VectorLayer `json:"vector_layers,omitempty"` // Optional. Populates the `vector_layers` field, describing the source layers in vector tiles
}

// Based off the TileJSON 3.0.0 vector_layers entry.
type VectorLayer struct {
	ID          string            `json:"id"`
	Fields      map[string]string `json:"fields"`
	Description string            `json:"description,omitempty"`
	MinZoom     *int              `json:"minzoom,omitempty"`
	MaxZoom     *int              `json:"maxzoom,omitempty"`
}

// The spec requires fields to be an object, so nil is written as {} rather than null.
func (v VectorLayer) MarshalJSON() ([]byte, error) {
	type plain VectorLayer
	if v.Fields == nil {
		v.Fields = map[string]string{}
	}

	return json.Marshal(plain(v))
}

// WithDefaults fills every field left unset in m from defaults.
func (m LayerMetadata) WithDefaults(defaults LayerMetadata) LayerMetadata {
	if m.DataType == "" || m.DataType == DataTypeUnknown {
		m.DataType = cmp.Or(defaults.DataType, m.DataType)
	}

	if m.MinZoom == nil {
		m.MinZoom = defaults.MinZoom
	}

	if m.MaxZoom == nil {
		m.MaxZoom = defaults.MaxZoom
	}

	if m.Bounds == (BoundsConfig{}) {
		m.Bounds = defaults.Bounds
	}

	m.Description = cmp.Or(m.Description, defaults.Description)
	m.Attribution = cmp.Or(m.Attribution, defaults.Attribution)
	m.Version = cmp.Or(m.Version, defaults.Version)

	if m.Center == nil {
		m.Center = defaults.Center
	}

	if m.VectorLayers == nil {
		m.VectorLayers = defaults.VectorLayers
	}

	return m
}
