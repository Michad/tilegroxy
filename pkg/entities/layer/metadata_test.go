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

type plainTestProvider struct {
	closed *atomic.Bool
}

func (p plainTestProvider) PreAuth(_ context.Context, pc ProviderContext) (ProviderContext, error) {
	return pc, nil
}

func (p plainTestProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{}, nil
}

func (p plainTestProvider) Close(_ context.Context) error {
	if p.closed != nil {
		p.closed.Store(true)
	}
	return nil
}

type metadataTestProvider struct {
	plainTestProvider
	md Description
}

func (p metadataTestProvider) Metadata() Description {
	return p.md
}

type parentTestProvider struct {
	metadataTestProvider
	children []Provider
}

func (p parentTestProvider) Children() []Provider {
	return p.children
}

type plainParentTestProvider struct {
	plainTestProvider
	children []Provider
}

func (p plainParentTestProvider) Children() []Provider {
	return p.children
}

func intPtr(i int) *int { return &i }

func Test_DescribeTree(t *testing.T) {
	a := metadataTestProvider{md: Description{DataType: config.DataTypeMVT, MinZoom: intPtr(2), TileJSONMetadata: config.TileJSONMetadata{Attribution: "a"}}}
	b := metadataTestProvider{md: Description{MinZoom: intPtr(4), TileJSONMetadata: config.TileJSONMetadata{Attribution: "b"}}}

	assert.Equal(t, Description{}, DescribeTree(nil))
	assert.Equal(t, Description{}, DescribeTree(plainTestProvider{}))
	assert.Equal(t, a.md, DescribeTree(a))
	assert.Equal(t, a.md, DescribeTree(plainParentTestProvider{children: []Provider{a}}))
	assert.Equal(t, Description{}, DescribeTree(plainParentTestProvider{}))
	assert.Equal(t, Union(a.md, b.md), DescribeTree(plainParentTestProvider{children: []Provider{a, b}}))
	assert.Equal(t, b.md, DescribeTree(parentTestProvider{metadataTestProvider: b, children: []Provider{a}}), "a MetadataProvider speaks for itself")
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

func Test_CloseProvider_WalksChildren(t *testing.T) {
	leafClosed := &atomic.Bool{}
	parentClosed := &atomic.Bool{}

	tree := plainParentTestProvider{plainTestProvider: plainTestProvider{closed: parentClosed}, children: []Provider{plainTestProvider{closed: leafClosed}}}

	require.NoError(t, CloseProvider(context.Background(), tree))

	assert.True(t, parentClosed.Load())
	assert.True(t, leafClosed.Load())
}
