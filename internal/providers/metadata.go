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
	"math"
	"slices"
	"strings"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

// Zoom and bounds stay unset unless every provider reports them, since the layer enforces them as limits.
func mergeMetadata(providers ...layer.Provider) config.LayerMetadata {
	var merged config.LayerMetadata
	var attributions []string

	for i, p := range providers {
		md := layer.MetadataOf(p)
		next := merged.WithDefaults(md)

		if md.Attribution != "" && !slices.Contains(attributions, md.Attribution) {
			attributions = append(attributions, md.Attribution)
		}

		next.VectorLayers = appendVectorLayers(merged.VectorLayers, md.VectorLayers)

		if i > 0 {
			next.MinZoom = combineZoom(merged.MinZoom, md.MinZoom, func(a, b int) int { return min(a, b) })
			next.MaxZoom = combineZoom(merged.MaxZoom, md.MaxZoom, func(a, b int) int { return max(a, b) })
			next.Bounds = unionBounds(merged.Bounds, md.Bounds)
		}

		merged = next
	}

	merged.Attribution = strings.Join(attributions, ", ")

	return merged
}

func combineZoom(a, b *int, pick func(int, int) int) *int {
	if a == nil || b == nil {
		return nil
	}

	z := pick(*a, *b)

	return &z
}

func unionBounds(a, b config.BoundsConfig) config.BoundsConfig {
	if a == (config.BoundsConfig{}) || b == (config.BoundsConfig{}) {
		return config.BoundsConfig{}
	}

	return config.BoundsConfig{
		South: math.Min(a.South, b.South),
		North: math.Max(a.North, b.North),
		West:  math.Min(a.West, b.West),
		East:  math.Max(a.East, b.East),
	}
}

// Layers sharing an id are kept once since TileJSON treats the id as the source-layer name.
func appendVectorLayers(existing, layers []config.VectorLayer) []config.VectorLayer {
	for _, l := range layers {
		if !slices.ContainsFunc(existing, func(e config.VectorLayer) bool { return e.ID == l.ID }) {
			existing = append(existing, l)
		}
	}

	return existing
}
