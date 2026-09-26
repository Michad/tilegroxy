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
	md     config.LayerMetadata
	closed *atomic.Bool
}

func (p metadataTestProvider) PreAuth(_ context.Context, pc ProviderContext) (ProviderContext, error) {
	return pc, nil
}

func (p metadataTestProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{}, nil
}

func (p metadataTestProvider) Metadata() config.LayerMetadata {
	return p.md
}

func (p metadataTestProvider) Close(_ context.Context) error {
	p.closed.Store(true)
	return nil
}

type metadataTestRegistration struct {
	name        string
	md          config.LayerMetadata
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

func constructMetadataLayer(t *testing.T, name string, md config.LayerMetadata, mutate func(*config.LayerConfig)) (*Layer, *atomic.Bool, error) {
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
	l, _, err := constructMetadataLayer(t, "md-dt-1", config.LayerMetadata{DataType: config.DataTypeMVT}, nil)

	require.NoError(t, err)
	assert.Equal(t, config.DataTypeMVT, l.DataType)
}

func Test_ConstructLayer_Metadata_MatchingConfiguredDataType(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-dt-2", config.LayerMetadata{DataType: config.DataTypeRaster}, func(c *config.LayerConfig) {
		c.DataType = config.DataTypeRaster
	})

	require.NoError(t, err)
	assert.Equal(t, config.DataTypeRaster, l.DataType)
}

func Test_ConstructLayer_Metadata_ContradictingDataTypeFailsAndCloses(t *testing.T) {
	l, closed, err := constructMetadataLayer(t, "md-dt-3", config.LayerMetadata{DataType: config.DataTypeMVT}, func(c *config.LayerConfig) {
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
	l, _, err := constructMetadataLayer(t, "md-dt-4", config.LayerMetadata{}, func(c *config.LayerConfig) {
		c.DataType = config.DataTypeRaster
	})

	require.NoError(t, err)
	assert.Equal(t, config.DataTypeRaster, l.DataType)
}

func Test_CheckZoomBounds_UsesMetadata(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-zoom-1", config.LayerMetadata{MinZoom: intPtr(3), MaxZoom: intPtr(8)}, nil)
	require.NoError(t, err)

	var rangeErr pkg.RangeError
	require.ErrorAs(t, l.CheckZoomBounds(pkg.TileRequest{Z: 2}), &rangeErr)
	assert.InDelta(t, 3, rangeErr.MinValue, 0)
	assert.InDelta(t, 8, rangeErr.MaxValue, 0)
	require.Error(t, l.CheckZoomBounds(pkg.TileRequest{Z: 9}))
	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 5}))
}

func Test_CheckZoomBounds_ConfigOverridesMetadata(t *testing.T) {
	l, _, err := constructMetadataLayer(t, "md-zoom-2", config.LayerMetadata{MinZoom: intPtr(3), MaxZoom: intPtr(8)}, func(c *config.LayerConfig) {
		c.MinZoom = intPtr(1)
		c.MaxZoom = intPtr(12)
	})
	require.NoError(t, err)

	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 2}))
	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 10}))
	require.Error(t, l.CheckZoomBounds(pkg.TileRequest{Z: 13}))
}

func Test_MetadataOf(t *testing.T) {
	md := config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Description: "d"}}

	assert.Equal(t, md, MetadataOf(metadataTestProvider{md: md}))
	assert.Equal(t, md, MetadataOf(ProviderWrapper{Name: "x", Provider: metadataTestProvider{md: md}}))
	assert.Equal(t, config.LayerMetadata{}, MetadataOf(ProviderWrapper{Name: "x", Provider: fixedTypeTestProvider{}}))
	assert.Equal(t, config.LayerMetadata{}, MetadataOf(nil))
}

func Test_Layer_Metadata_ConfigOverridesProvider(t *testing.T) {
	md := config.LayerMetadata{MinZoom: intPtr(3), MaxZoom: intPtr(8), TileJSONMetadata: config.TileJSONMetadata{Description: "provider", Attribution: "provider", Version: "1"}}

	l, _, err := constructMetadataLayer(t, "md-layer-1", md, func(c *config.LayerConfig) {
		c.DataType = config.DataTypeMVT
		c.MaxZoom = intPtr(12)
		c.Attribution = "layer"
	})
	require.NoError(t, err)

	l.Config.Bounds = config.BoundsConfig{South: 1, North: 2, West: 3, East: 4}

	got := l.Metadata()

	assert.Equal(t, config.DataTypeMVT, got.DataType)
	assert.Equal(t, 3, *got.MinZoom)
	assert.Equal(t, 12, *got.MaxZoom)
	assert.Equal(t, "provider", got.Description)
	assert.Equal(t, "layer", got.Attribution)
	assert.Equal(t, "1", got.Version)
	assert.Equal(t, config.BoundsConfig{South: 1, North: 2, West: 3, East: 4}, got.Bounds)
}
