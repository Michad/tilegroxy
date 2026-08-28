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
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

// TileJSONDocument mirrors the fields tilegroxy populates from the TileJSON 3.0.0 spec:
// https://github.com/mapbox/tilejson-spec/tree/master/3.0.0
type TileJSONDocument struct {
	TileJSON    string    `json:"tilejson"`
	Name        string    `json:"name"`
	Tiles       []string  `json:"tiles"`
	MinZoom     int       `json:"minzoom"`
	MaxZoom     int       `json:"maxzoom"`
	Bounds      []float64 `json:"bounds"`
	Description string    `json:"description,omitempty"`
	Attribution string    `json:"attribution,omitempty"`
}

const tileJSONVersion = "3.0.0"

// TileJSONEligible reports whether this layer can produce at least one TileJSON document: a plain
// id layer always qualifies, a pattern layer only if it configures Examples.
func (l *Layer) TileJSONEligible() bool {
	if !l.IsPattern() {
		return true
	}

	return len(l.Config.Examples) > 0
}

// TileJSONNames returns the name(s) this layer produces TileJSON documents under: its own ID for
// a plain layer, or its configured Examples for a pattern layer.
func (l *Layer) TileJSONNames() []string {
	if !l.IsPattern() {
		return []string{l.ID}
	}

	return l.Config.Examples
}

// BuildTileJSON constructs the TileJSON document for this layer under the given name (either the
// layer's own ID or one of its Examples), intersecting the layer's configured bounds with a
// caller-specific allowedArea when one applies. tilesURLs are the fully-formed `{z}/{x}/{y}` tile
// URLs for this specific name, built by the caller from the request's own scheme/host/path or the
// configured BaseURLs.
func (l *Layer) BuildTileJSON(name string, tilesURLs []string, allowedArea *pkg.Bounds) TileJSONDocument {
	minZoom := 0
	if l.Config.MinZoom != nil {
		minZoom = *l.Config.MinZoom
	}

	maxZoom := pkg.MaxZoom
	if l.Config.MaxZoom != nil {
		maxZoom = *l.Config.MaxZoom
	}

	bounds := pkg.WorldBounds()
	if l.Config.Bounds != (config.BoundsConfig{}) {
		bounds = pkg.Bounds{
			South: l.Config.Bounds.South,
			North: l.Config.Bounds.North,
			West:  l.Config.Bounds.West,
			East:  l.Config.Bounds.East,
			SRID:  pkg.SRIDWGS84,
		}
	}

	if allowedArea != nil && !allowedArea.IsNullIsland() {
		bounds = bounds.IntersectionWith(*allowedArea)
	}

	doc := TileJSONDocument{
		TileJSON:    tileJSONVersion,
		Name:        name,
		Tiles:       tilesURLs,
		MinZoom:     minZoom,
		MaxZoom:     maxZoom,
		Bounds:      []float64{bounds.West, bounds.South, bounds.East, bounds.North},
		Description: l.Config.Description,
		Attribution: l.Config.Attribution,
	}

	return doc
}
