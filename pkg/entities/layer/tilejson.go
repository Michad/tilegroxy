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

const tileJSONVersion = "3.0.0"

// Based off the TileJSON 3.0.0 spec: https://github.com/mapbox/tilejson-spec/tree/master/3.0.0
type TileJSONDocument struct {
	TileJSON string    `json:"tilejson"` // Version
	Name     string    `json:"name"`
	Tiles    []string  `json:"tiles"`
	MinZoom  int       `json:"minzoom"`
	MaxZoom  int       `json:"maxzoom"`
	Bounds   []float64 `json:"bounds"`
	config.TileJSONMetadata
}

func (l *Layer) TileJSONEligible() bool {
	if !l.IsPattern() {
		return true
	}

	return len(l.Config.Examples) > 0
}

func (l *Layer) TileJSONNames() []string {
	if !l.IsPattern() {
		return []string{l.ID}
	}

	return l.Config.Examples
}

func (l *Layer) BuildTileJSON(name string, tilesURLs []string, allowedArea *pkg.Bounds) TileJSONDocument {
	minZoom, maxZoom := l.zoomRange()
	md := l.Metadata()

	bounds := pkg.WorldBounds()
	if md.Bounds != (config.BoundsConfig{}) {
		bounds = pkg.BoundsFromConfig(md.Bounds)
	}

	if allowedArea != nil && !allowedArea.IsNullIsland() {
		bounds = bounds.IntersectionWith(*allowedArea)
	}

	return TileJSONDocument{
		TileJSON:         tileJSONVersion,
		Name:             name,
		Tiles:            tilesURLs,
		MinZoom:          minZoom,
		MaxZoom:          maxZoom,
		Bounds:           []float64{bounds.West, bounds.South, bounds.East, bounds.North},
		TileJSONMetadata: md.TileJSONMetadata,
	}
}
