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
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

// What a provider knows about its tiles. Advertised to clients but never enforced
type Description struct {
	DataType config.DataType
	MinZoom  *int
	MaxZoom  *int
	Bounds   config.BoundsConfig
	config.TileJSONMetadata
}

// Optional, for providers that can describe their own tiles
type MetadataProvider interface {
	Metadata() Description
}

// Providers that nest others, so the tree can be walked generically
type Parent interface {
	Children() []Provider
}

// A MetadataProvider speaks for itself, a single child passes through, and several are unioned
func DescribeTree(p Provider) Description {
	if m, ok := p.(MetadataProvider); ok {
		return m.Metadata()
	}

	parent, ok := p.(Parent)
	if !ok {
		return Description{}
	}

	children := parent.Children()
	if len(children) == 1 {
		return DescribeTree(children[0])
	}

	descriptions := make([]Description, 0, len(children))
	for _, c := range children {
		descriptions = append(descriptions, DescribeTree(c))
	}

	return Union(descriptions...)
}

// A field is unknown if any description leaves it unknown, since that source could cover anything
func Union(descriptions ...Description) Description {
	if len(descriptions) == 0 {
		return Description{}
	}

	merged := descriptions[0]
	merged.VectorLayers = appendVectorLayers(nil, merged.VectorLayers)
	attributions := appendUnique(nil, merged.Attribution)

	for _, d := range descriptions[1:] {
		merged.DataType = unionDataType(merged.DataType, d.DataType)
		merged.MinZoom = combineZoom(merged.MinZoom, d.MinZoom, func(a, b int) int { return min(a, b) })
		merged.MaxZoom = combineZoom(merged.MaxZoom, d.MaxZoom, func(a, b int) int { return max(a, b) })
		merged.Bounds = unionBounds(merged.Bounds, d.Bounds)
		merged.Description = cmp.Or(merged.Description, d.Description)
		merged.Version = cmp.Or(merged.Version, d.Version)
		merged.VectorLayers = appendVectorLayers(merged.VectorLayers, d.VectorLayers)
		attributions = appendUnique(attributions, d.Attribution)

		if merged.Center == nil {
			merged.Center = d.Center
		}
	}

	merged.Attribution = strings.Join(attributions, ", ")

	return merged
}

// Drops a center that falls outside the bounds
func (d Description) Clip(bounds config.BoundsConfig) Description {
	if bounds == (config.BoundsConfig{}) {
		return d
	}

	limit := pkg.BoundsFromConfig(bounds)
	clipped := limit
	if reported := pkg.BoundsFromConfig(d.Bounds); d.Bounds != (config.BoundsConfig{}) && reported.Intersects(limit) {
		clipped = reported.IntersectionWith(limit)
	}
	d.Bounds = clipped.ToConfig()

	if !centerWithin(d.Center, d.Bounds) {
		d.Center = nil
	}

	return d
}

// Unset ends are filled in
func (d Description) ZoomRange() (int, int) {
	return zoomRange(d.MinZoom, d.MaxZoom)
}

func zoomRange(minZoom, maxZoom *int) (int, int) {
	lo, hi := 0, pkg.MaxZoom
	if minZoom != nil {
		lo = *minZoom
	}

	if maxZoom != nil {
		hi = *maxZoom
	}

	return lo, hi
}

// Also closes every provider nested beneath it
func CloseProvider(ctx context.Context, p Provider) error {
	errs := []error{lifecycle.CloseIfCloser(ctx, p)}

	if parent, ok := p.(Parent); ok {
		for _, c := range parent.Children() {
			errs = append(errs, CloseProvider(ctx, c))
		}
	}

	return errors.Join(errs...)
}

// Longitude, latitude, then an optional zoom
const centerLatIndex = 1

func centerWithin(center []float64, bounds config.BoundsConfig) bool {
	if len(center) <= centerLatIndex || bounds == (config.BoundsConfig{}) {
		return true
	}

	lon, lat := center[0], center[centerLatIndex]

	return lon >= bounds.West && lon <= bounds.East && lat >= bounds.South && lat <= bounds.North
}

// Unknown types are ignored, but two known types that disagree leave the union unknown
func unionDataType(a, b config.DataType) config.DataType {
	switch {
	case !isKnownDataType(a):
		return b
	case !isKnownDataType(b) || a == b:
		return a
	default:
		return config.DataTypeUnknown
	}
}

func isKnownDataType(d config.DataType) bool {
	return d != "" && d != config.DataTypeUnknown
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

func appendUnique(existing []string, s string) []string {
	if s == "" || slices.Contains(existing, s) {
		return existing
	}

	return append(existing, s)
}

// Layers sharing an id are kept once since TileJSON treats the id as the source-layer name
func appendVectorLayers(existing, layers []config.VectorLayer) []config.VectorLayer {
	for _, l := range layers {
		if !slices.ContainsFunc(existing, func(e config.VectorLayer) bool { return e.ID == l.ID }) {
			existing = append(existing, l)
		}
	}

	return existing
}
