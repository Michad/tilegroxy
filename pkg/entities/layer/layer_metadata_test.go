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
	"context"
	"sync/atomic"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metadataTestProvider struct {
	md     Description
	closed *atomic.Bool
}

func (p metadataTestProvider) PreAuth(_ context.Context, pc ProviderContext) (ProviderContext, error) {
	return pc, nil
}

func (p metadataTestProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{}, nil
}

func (p metadataTestProvider) Metadata() Description {
	return p.md
}

func (p metadataTestProvider) Close(_ context.Context) error {
	if p.closed != nil {
		p.closed.Store(true)
	}
	return nil
}

type metadataTestRegistration struct {
	name        string
	md          Description
	closed      *atomic.Bool
	constructed *atomic.Bool
}

func (r metadataTestRegistration) InitializeConfig() any { return struct{}{} }

func (r metadataTestRegistration) Name() string { return r.name }

func (r metadataTestRegistration) DataType(_ any) config.DataType { return config.DataTypeUnknown }

func (r metadataTestRegistration) Initialize(_ any, _ ProviderDeps) (Provider, error) {
	if r.constructed != nil {
		r.constructed.Store(true)
	}
	return metadataTestProvider{md: r.md, closed: r.closed}, nil
}

var metadataErrorMessages = config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}

func intPtr(i int) *int { return &i }

func constructMetadataLayer(t *testing.T, name string, md Description, mutate func(*config.LayerConfig)) (*Layer, *atomic.Bool, error) {
	t.Helper()

	closed := &atomic.Bool{}
	RegisterProvider(metadataTestRegistration{name: name, md: md, closed: closed})

	rawConfig := config.LayerConfig{ID: name, Provider: map[string]any{"name": name}}
	if mutate != nil {
		mutate(&rawConfig)
	}

	l, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, metadataErrorMessages, nil, nil, nil)
	return l, closed, err
}

func Test_ConstructLayer_Metadata_FillsUnknownDataType(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-dt-1", Description{DataType: config.DataTypeMVT}, nil)

	require.NoError(t, err)
	assert.Equal(t, config.DataTypeMVT, l.DataType)
}

func Test_ConstructLayer_Metadata_MatchingConfiguredDataType(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-dt-2", Description{DataType: config.DataTypeRaster}, func(c *config.LayerConfig) {
		c.DataType = config.DataTypeRaster
	})

	require.NoError(t, err)
	assert.Equal(t, config.DataTypeRaster, l.DataType)
}

func Test_ConstructLayer_Metadata_ContradictingDataTypeFailsAndCloses(t *testing.T) {
	l, closed, err := constructMetadataLayer(t, "md-dt-3", Description{DataType: config.DataTypeMVT}, func(c *config.LayerConfig) {
		c.DataType = config.DataTypeRaster
	})

	require.Error(t, err)
	assert.Nil(t, l)
	assert.True(t, closed.Load())
}

func Test_ConstructLayer_BadPattern_NeverConstructsProvider(t *testing.T) {
	closed := &atomic.Bool{}
	constructed := &atomic.Bool{}
	RegisterProvider(metadataTestRegistration{name: "md-pattern-1", closed: closed, constructed: constructed})

	rawConfig := config.LayerConfig{
		ID:       "md-pattern-1",
		Pattern:  "{unterminated",
		Provider: map[string]any{"name": "md-pattern-1"},
	}

	l, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, metadataErrorMessages, nil, nil, nil)

	require.Error(t, err)
	assert.Nil(t, l)
	assert.False(t, constructed.Load(), "provider should never be constructed when the pattern fails to parse")
	assert.False(t, closed.Load())
}

func Test_ConstructLayer_Metadata_UnknownMetadataKeepsConfig(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-dt-4", Description{}, func(c *config.LayerConfig) {
		c.DataType = config.DataTypeRaster
	})

	require.NoError(t, err)
	assert.Equal(t, config.DataTypeRaster, l.DataType)
}

// Provider zooms are descriptive, so a fallback whose primary stops at z8 still serves z9 from its secondary.
func Test_CheckZoomBounds_IgnoresProviderZoom(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-zoom-1", Description{MinZoom: intPtr(3), MaxZoom: intPtr(8)}, nil)
	require.NoError(t, err)

	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 2}))
	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 9}))

	minZoom, maxZoom := l.Metadata().Advertised.ZoomRange()
	assert.Equal(t, 3, minZoom)
	assert.Equal(t, 8, maxZoom)
}

func Test_CheckZoomBounds_EnforcesConfig(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-zoom-2", Description{MinZoom: intPtr(3), MaxZoom: intPtr(8)}, func(c *config.LayerConfig) {
		c.MinZoom = intPtr(1)
		c.MaxZoom = intPtr(12)
	})
	require.NoError(t, err)

	var rangeErr pkg.RangeError
	require.ErrorAs(t, l.CheckZoomBounds(pkg.TileRequest{Z: 0}), &rangeErr)
	assert.InDelta(t, 1, rangeErr.MinValue, 0)
	assert.InDelta(t, 12, rangeErr.MaxValue, 0)
	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 10}))
	require.Error(t, l.CheckZoomBounds(pkg.TileRequest{Z: 13}))
}

func Test_Layer_Metadata_ConfigOverridesProvider(t *testing.T) {
	md := Description{MinZoom: intPtr(3), MaxZoom: intPtr(8), TileJSONMetadata: config.TileJSONMetadata{Description: "provider", Attribution: "provider", Version: "1"}}

	l, _, err := constructMetadataLayer(t, "md-layer-1", md, func(c *config.LayerConfig) {
		c.DataType = config.DataTypeMVT
		c.MaxZoom = intPtr(12)
		c.Attribution = "layer"
	})
	require.NoError(t, err)

	got := l.Metadata()

	assert.Equal(t, Limits{MaxZoom: intPtr(12)}, got.Limits)
	assert.Equal(t, config.DataTypeMVT, got.Advertised.DataType)
	assert.Equal(t, 3, *got.Advertised.MinZoom)
	assert.Equal(t, 12, *got.Advertised.MaxZoom)
	assert.Equal(t, "provider", got.Advertised.Description)
	assert.Equal(t, "layer", got.Advertised.Attribution)
	assert.Equal(t, "1", got.Advertised.Version)
}

type resolveCase struct {
	name     string
	cfg      config.LayerMetadata
	reported Description
	want     Description
}

var tenToTwenty = config.BoundsConfig{South: 10, North: 20, West: 10, East: 20}

func runResolveCases(t *testing.T, tests []resolveCase) {
	t.Helper()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.want.DataType = config.DataTypeUnknown
			got, err := resolveMetadata("l", tc.cfg, tc.reported, metadataErrorMessages)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Advertised)
			assert.Equal(t, Limits{MinZoom: tc.cfg.MinZoom, MaxZoom: tc.cfg.MaxZoom, Bounds: tc.cfg.Bounds}, got.Limits)
		})
	}
}

func Test_ResolveMetadata_ZoomAndBounds(t *testing.T) {
	runResolveCases(t, []resolveCase{
		{
			name:     "provider only",
			reported: Description{MinZoom: intPtr(2), MaxZoom: intPtr(9), Bounds: tenToTwenty, TileJSONMetadata: config.TileJSONMetadata{Center: []float64{15, 15, 4}}},
			want:     Description{MinZoom: intPtr(2), MaxZoom: intPtr(9), Bounds: tenToTwenty, TileJSONMetadata: config.TileJSONMetadata{Center: []float64{15, 15, 4}}},
		},
		{
			name:     "provider minzoom above layer maxzoom is ignored",
			cfg:      config.LayerMetadata{MaxZoom: intPtr(5)},
			reported: Description{MinZoom: intPtr(8)},
			want:     Description{MaxZoom: intPtr(5)},
		},
		{
			name:     "provider maxzoom below layer minzoom is ignored",
			cfg:      config.LayerMetadata{MinZoom: intPtr(10)},
			reported: Description{MaxZoom: intPtr(8)},
			want:     Description{MinZoom: intPtr(10)},
		},
		{
			name:     "provider zoom range inverted keeps minzoom",
			reported: Description{MinZoom: intPtr(10), MaxZoom: intPtr(8)},
			want:     Description{MinZoom: intPtr(10)},
		},
		{
			name:     "provider bounds are intersected with layer bounds",
			cfg:      config.LayerMetadata{Bounds: config.BoundsConfig{South: 15, North: 30, West: 15, East: 30}},
			reported: Description{Bounds: tenToTwenty},
			want:     Description{Bounds: config.BoundsConfig{South: 15, North: 20, West: 15, East: 20}},
		},
		{
			name:     "disjoint provider bounds are ignored",
			cfg:      config.LayerMetadata{Bounds: config.BoundsConfig{South: 40, North: 50, West: 40, East: 50}},
			reported: Description{Bounds: tenToTwenty},
			want:     Description{Bounds: config.BoundsConfig{South: 40, North: 50, West: 40, East: 50}},
		},
		{
			name:     "invalid provider bounds are ignored",
			reported: Description{Bounds: config.BoundsConfig{South: 20, North: 10, West: 10, East: 20}},
			want:     Description{},
		},
	})
}

func Test_ResolveMetadata_Center(t *testing.T) {
	runResolveCases(t, []resolveCase{
		{
			name:     "provider center outside bounds is dropped",
			cfg:      config.LayerMetadata{Bounds: tenToTwenty},
			reported: Description{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{50, 50}}},
			want:     Description{Bounds: tenToTwenty},
		},
		{
			name:     "provider center zoom is clamped to the zoom range",
			cfg:      config.LayerMetadata{MaxZoom: intPtr(6)},
			reported: Description{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 12}}},
			want:     Description{MaxZoom: intPtr(6), TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 6}}},
		},
		{
			name:     "malformed provider center is dropped",
			reported: Description{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1}}},
			want:     Description{},
		},
		{
			name:     "layer center wins over provider bounds excluding it",
			cfg:      config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{50, 50}}},
			reported: Description{Bounds: tenToTwenty},
			want:     Description{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{50, 50}}},
		},
		{
			name:     "layer center zoom wins over provider zoom excluding it",
			cfg:      config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 12}}},
			reported: Description{MinZoom: intPtr(14), MaxZoom: intPtr(10)},
			want:     Description{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 12}}},
		},
		{
			name:     "layer center zoom below provider minzoom drops it",
			cfg:      config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 3}}},
			reported: Description{MinZoom: intPtr(5), MaxZoom: intPtr(10)},
			want:     Description{MaxZoom: intPtr(10), TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 3}}},
		},
		{
			name:     "layer center zoom above provider maxzoom drops it",
			cfg:      config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 12}}},
			reported: Description{MinZoom: intPtr(5), MaxZoom: intPtr(10)},
			want:     Description{MinZoom: intPtr(5), TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 2, 12}}},
		},
		{
			name:     "layer center outside provider bounds falls back to layer bounds",
			cfg:      config.LayerMetadata{Bounds: config.BoundsConfig{South: 0, North: 30, West: 0, East: 30}, TileJSONMetadata: config.TileJSONMetadata{Center: []float64{25, 25}}},
			reported: Description{Bounds: tenToTwenty},
			want:     Description{Bounds: config.BoundsConfig{South: 0, North: 30, West: 0, East: 30}, TileJSONMetadata: config.TileJSONMetadata{Center: []float64{25, 25}}},
		},
		{
			name:     "layer vector layers override the provider's",
			cfg:      config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{VectorLayers: []config.VectorLayer{{ID: "water"}}}},
			reported: Description{TileJSONMetadata: config.TileJSONMetadata{VectorLayers: []config.VectorLayer{{ID: "roads"}}}},
			want:     Description{TileJSONMetadata: config.TileJSONMetadata{VectorLayers: []config.VectorLayer{{ID: "water"}}}},
		},
	})
}

type parentTestProvider struct {
	metadataTestProvider
	children []Provider
}

func (p parentTestProvider) Children() []Provider {
	return p.children
}

type plainParentTestProvider struct {
	fixedTypeTestProvider
	children []Provider
	closed   *atomic.Bool
}

func (p plainParentTestProvider) Children() []Provider {
	return p.children
}

func (p plainParentTestProvider) Close(_ context.Context) error {
	p.closed.Store(true)
	return nil
}

func Test_DescribeTree(t *testing.T) {
	a := metadataTestProvider{md: Description{DataType: config.DataTypeMVT, MinZoom: intPtr(2), TileJSONMetadata: config.TileJSONMetadata{Attribution: "a"}}}
	b := metadataTestProvider{md: Description{MinZoom: intPtr(4), TileJSONMetadata: config.TileJSONMetadata{Attribution: "b"}}}

	assert.Equal(t, Description{}, DescribeTree(nil))
	assert.Equal(t, Description{}, DescribeTree(fixedTypeTestProvider{}))
	assert.Equal(t, a.md, DescribeTree(a))
	assert.Equal(t, a.md, DescribeTree(plainParentTestProvider{children: []Provider{a}}))
	assert.Equal(t, Description{}, DescribeTree(plainParentTestProvider{}))
	assert.Equal(t, Union(a.md, b.md), DescribeTree(plainParentTestProvider{children: []Provider{a, b}}))
	assert.Equal(t, b.md, DescribeTree(parentTestProvider{metadataTestProvider: b, children: []Provider{a}}), "a MetadataProvider speaks for itself")
}

func Test_ProviderWrapper_Metadata_RegistrationTypeWins(t *testing.T) {
	inner := metadataTestProvider{md: Description{DataType: config.DataTypeMVT, MinZoom: intPtr(2)}}

	assert.Equal(t, inner.md, ProviderWrapper{Provider: inner}.Metadata())
	assert.Equal(t, inner.md, ProviderWrapper{Provider: inner, dataType: config.DataTypeUnknown}.Metadata())
	assert.Equal(t, config.DataTypeRaster, ProviderWrapper{Provider: inner, dataType: config.DataTypeRaster}.Metadata().DataType)
}

func Test_Union(t *testing.T) {
	roads := config.VectorLayer{ID: "roads"}
	water := config.VectorLayer{ID: "water"}

	tests := []struct {
		name string
		in   []Description
		want Description
	}{
		{name: "empty", want: Description{}},
		{
			name: "single passes through",
			in:   []Description{{MinZoom: intPtr(3), TileJSONMetadata: config.TileJSONMetadata{Attribution: "a"}}},
			want: Description{MinZoom: intPtr(3), TileJSONMetadata: config.TileJSONMetadata{Attribution: "a"}},
		},
		{
			name: "zoom widens",
			in:   []Description{{MinZoom: intPtr(3), MaxZoom: intPtr(8)}, {MinZoom: intPtr(5), MaxZoom: intPtr(14)}},
			want: Description{MinZoom: intPtr(3), MaxZoom: intPtr(14)},
		},
		{
			name: "unknown zoom makes the union unknown",
			in:   []Description{{MinZoom: intPtr(3), MaxZoom: intPtr(8)}, {MaxZoom: intPtr(14)}},
			want: Description{MaxZoom: intPtr(14)},
		},
		{
			name: "bounds widen",
			in:   []Description{{Bounds: config.BoundsConfig{South: 0, North: 1, West: 0, East: 1}}, {Bounds: config.BoundsConfig{South: -1, North: 0.5, West: 0.5, East: 2}}},
			want: Description{Bounds: config.BoundsConfig{South: -1, North: 1, West: 0, East: 2}},
		},
		{
			name: "unknown bounds make the union unknown",
			in:   []Description{{Bounds: config.BoundsConfig{South: 0, North: 1, West: 0, East: 1}}, {}},
			want: Description{},
		},
		{
			name: "known data types that agree are kept",
			in:   []Description{{DataType: config.DataTypeUnknown}, {DataType: config.DataTypeMVT}, {DataType: config.DataTypeMVT}},
			want: Description{DataType: config.DataTypeMVT},
		},
		{
			name: "conflicting data types are unknown",
			in:   []Description{{DataType: config.DataTypeRaster}, {DataType: config.DataTypeMVT}},
			want: Description{DataType: config.DataTypeUnknown},
		},
		{
			name: "text fields take the first set and attributions join",
			in: []Description{
				{TileJSONMetadata: config.TileJSONMetadata{Attribution: "a", VectorLayers: []config.VectorLayer{roads}}},
				{TileJSONMetadata: config.TileJSONMetadata{Attribution: "b", Description: "d", Version: "1", Center: []float64{1, 2}, VectorLayers: []config.VectorLayer{roads, water}}},
				{TileJSONMetadata: config.TileJSONMetadata{Attribution: "a", Description: "e", Version: "2", Center: []float64{3, 4}}},
			},
			want: Description{TileJSONMetadata: config.TileJSONMetadata{Attribution: "a, b", Description: "d", Version: "1", Center: []float64{1, 2}, VectorLayers: []config.VectorLayer{roads, water}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Union(tc.in...))
		})
	}
}

func Test_Description_Clip(t *testing.T) {
	limit := config.BoundsConfig{South: 0, North: 10, West: 0, East: 10}

	d := Description{Bounds: config.BoundsConfig{South: 5, North: 15, West: 5, East: 15}, TileJSONMetadata: config.TileJSONMetadata{Center: []float64{12, 12}}}
	got := d.Clip(limit)
	assert.Equal(t, config.BoundsConfig{South: 5, North: 10, West: 5, East: 10}, got.Bounds)
	assert.Nil(t, got.Center)

	assert.Equal(t, limit, Description{}.Clip(limit).Bounds)
	assert.Equal(t, limit, Description{Bounds: config.BoundsConfig{South: 50, North: 60, West: 50, East: 60}}.Clip(limit).Bounds)
	assert.Equal(t, d, d.Clip(config.BoundsConfig{}))
}

func Test_ResolvedMetadata_ForRequest(t *testing.T) {
	r := ResolvedMetadata{Advertised: Description{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{1, 1}}}}

	assert.Equal(t, r.Advertised, r.ForRequest(nil))
	assert.Equal(t, r.Advertised, r.ForRequest(&pkg.Bounds{}))

	got := r.ForRequest(&pkg.Bounds{South: 0, North: 2, West: 0, East: 2, SRID: pkg.SRIDWGS84})
	assert.Equal(t, config.BoundsConfig{South: 0, North: 2, West: 0, East: 2}, got.Bounds)
	assert.Equal(t, []float64{1, 1}, got.Center)

	got = r.ForRequest(&pkg.Bounds{South: 5, North: 6, West: 5, East: 6, SRID: pkg.SRIDWGS84})
	assert.Nil(t, got.Center)
}

func Test_CloseProvider_WalksChildren(t *testing.T) {
	leafClosed := &atomic.Bool{}
	parentClosed := &atomic.Bool{}
	wrappedClosed := &atomic.Bool{}

	leaf := metadataTestProvider{closed: leafClosed}
	wrapped := ProviderWrapper{Provider: metadataTestProvider{closed: wrappedClosed}}
	tree := plainParentTestProvider{children: []Provider{leaf, wrapped}, closed: parentClosed}

	require.NoError(t, CloseProvider(context.Background(), ProviderWrapper{Provider: tree}))

	assert.True(t, parentClosed.Load())
	assert.True(t, leafClosed.Load())
	assert.True(t, wrappedClosed.Load())
}

func Test_BuildOrder_RefTargetsFirst(t *testing.T) {
	ref := func(target string) map[string]any { return map[string]any{"name": "ref", "layer": target} }
	layers := []config.LayerConfig{
		{ID: "a", Provider: map[string]any{"name": "fallback", "primary": ref("b"), "secondary": ref("tile_x")}},
		{ID: "b", Provider: ref("c")},
		{ID: "c", Provider: map[string]any{"name": "static"}},
		{ID: "pattern", Pattern: "tile_{name}", Provider: ref("tile_y")},
	}

	assert.Equal(t, []int{2, 1, 3, 0}, buildOrder(layers, metadataErrorMessages))
}

func Test_ExtractDataType(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-extract-mvt", dt: config.DataTypeMVT})

	assert.Equal(t, config.DataTypeMVT, ExtractDataType(map[string]any{"name": "fixed-extract-mvt"}))
	assert.Equal(t, config.DataTypeUnknown, ExtractDataType(map[string]any{"name": "not-registered"}))
	assert.Equal(t, config.DataTypeUnknown, ExtractDataType(map[string]any{}))
}
