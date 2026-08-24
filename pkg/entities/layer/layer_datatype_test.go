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
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

type fixedTypeTestProvider struct {
	dt config.DataType
}

func (p fixedTypeTestProvider) PreAuth(_ context.Context, pc ProviderContext) (ProviderContext, error) {
	return pc, nil
}

func (p fixedTypeTestProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

type fixedTypeTestRegistration struct {
	name string
	dt   config.DataType
}

func (r fixedTypeTestRegistration) InitializeConfig() any {
	return struct{}{}
}

func (r fixedTypeTestRegistration) Name() string {
	return r.name
}

func (r fixedTypeTestRegistration) DataType(_ any) config.DataType {
	return r.dt
}

func (r fixedTypeTestRegistration) Initialize(_ any, _ ProviderDeps) (Provider, error) {
	return fixedTypeTestProvider{dt: r.dt}, nil
}

func Test_ConstructLayer_DataType_MatchingExplicitAndProviderType_Succeeds(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-raster-1", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "l1",
		DataType: config.DataTypeRaster,
		Provider: map[string]any{"name": "fixed-raster-1"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
}

func Test_ConstructLayer_DataType_ContradictoryExplicitAndProviderType_Fails(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-mvt-1", dt: config.DataTypeMVT})

	rawConfig := config.LayerConfig{
		ID:       "l2",
		DataType: config.DataTypeRaster,
		Provider: map[string]any{"name": "fixed-mvt-1"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.Error(t, err)
	require.Nil(t, l)
}

func Test_ConstructLayer_DataType_ProviderUnknownNoExplicitType_Succeeds(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-unknown-1", dt: config.DataTypeUnknown})

	rawConfig := config.LayerConfig{
		ID:       "l3",
		Provider: map[string]any{"name": "fixed-unknown-1"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
}

func Test_ConstructLayer_Bounds_WithUnresolvableDataType_Fails(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-unknown-2", dt: config.DataTypeUnknown})

	rawConfig := config.LayerConfig{
		ID:       "l4",
		Bounds:   config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{"name": "fixed-unknown-2"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.Error(t, err)
	require.Nil(t, l)
}

func Test_ConstructLayer_Bounds_WithExplicitDataType_Succeeds(t *testing.T) {
	RegisterProvider(wrapMarkerRegistration{name: "crop"})
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-unknown-3", dt: config.DataTypeUnknown})

	rawConfig := config.LayerConfig{
		ID:       "l5",
		DataType: config.DataTypeRaster,
		Bounds:   config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{"name": "fixed-unknown-3"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
}

func Test_ConstructLayer_Bounds_WithResolvableProviderType_Succeeds(t *testing.T) {
	RegisterProvider(wrapMarkerRegistration{name: "cropmvt"})
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-mvt-2", dt: config.DataTypeMVT})

	rawConfig := config.LayerConfig{
		ID:       "l6",
		Bounds:   config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{"name": "fixed-mvt-2"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
}

type wrapMarkerProvider struct {
	primary Provider
	name    string
}

func (p wrapMarkerProvider) PreAuth(ctx context.Context, pc ProviderContext) (ProviderContext, error) {
	return p.primary.PreAuth(ctx, pc)
}

func (p wrapMarkerProvider) GenerateTile(ctx context.Context, pc ProviderContext, tr pkg.TileRequest) (*pkg.Image, error) {
	return p.primary.GenerateTile(ctx, pc, tr)
}

type wrapMarkerConfig struct {
	Primary map[string]interface{}
	Bounds  pkg.Bounds
}

type wrapMarkerRegistration struct {
	name string
}

func (r wrapMarkerRegistration) InitializeConfig() any {
	return wrapMarkerConfig{}
}

func (r wrapMarkerRegistration) Name() string {
	return r.name
}

func (r wrapMarkerRegistration) DataType(cfgAny any) config.DataType {
	cfg := cfgAny.(wrapMarkerConfig)
	return ExtractDataType(cfg.Primary)
}

func (r wrapMarkerRegistration) Initialize(cfgAny any, deps ProviderDeps) (Provider, error) {
	cfg := cfgAny.(wrapMarkerConfig)
	primary, err := ConstructProvider(cfg.Primary, deps)
	if err != nil {
		return nil, err
	}
	return wrapMarkerProvider{primary: primary, name: r.name}, nil
}

func Test_ConstructLayer_Bounds_Raster_WrapsInCrop(t *testing.T) {
	RegisterProvider(wrapMarkerRegistration{name: "crop"})
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-raster-2", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "l7",
		Bounds:   config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{"name": "fixed-raster-2"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
	wrapper, ok := l.Provider.(ProviderWrapper)
	require.True(t, ok)
	require.Equal(t, "crop", wrapper.Name)
}

func Test_ConstructLayer_Bounds_MVT_WrapsInCropMvt(t *testing.T) {
	RegisterProvider(wrapMarkerRegistration{name: "cropmvt"})
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-mvt-3", dt: config.DataTypeMVT})

	rawConfig := config.LayerConfig{
		ID:       "l8",
		Bounds:   config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{"name": "fixed-mvt-3"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
	wrapper, ok := l.Provider.(ProviderWrapper)
	require.True(t, ok)
	require.Equal(t, "cropmvt", wrapper.Name)
}

func Test_ConstructLayer_NoBounds_NoWrapping(t *testing.T) {
	RegisterProvider(fixedTypeTestRegistration{name: "fixed-raster-3", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:       "l9",
		Provider: map[string]any{"name": "fixed-raster-3"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
	// Unwrapped: the provider wrapper's Name is the literal provider name, not "crop"
	wrapper, ok := l.Provider.(ProviderWrapper)
	require.True(t, ok)
	require.Equal(t, "fixed-raster-3", wrapper.Name)
}

// closableTypedProvider is like closableProvider (layergroup_test.go) but registered under a
// caller-supplied data type, needed here to be resolvable enough to trigger bounds wrapping.
type closableTypedProvider struct {
	closed *bool
}

func (p closableTypedProvider) Close(_ context.Context) error {
	*p.closed = true
	return nil
}

func (p closableTypedProvider) PreAuth(_ context.Context, pc ProviderContext) (ProviderContext, error) {
	return pc, nil
}

func (p closableTypedProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

type closableTypedTestRegistration struct {
	name   string
	dt     config.DataType
	closed *bool
}

func (r closableTypedTestRegistration) InitializeConfig() any {
	return struct{}{}
}

func (r closableTypedTestRegistration) Name() string {
	return r.name
}

func (r closableTypedTestRegistration) DataType(_ any) config.DataType {
	return r.dt
}

func (r closableTypedTestRegistration) Initialize(_ any, _ ProviderDeps) (Provider, error) {
	return closableTypedProvider{closed: r.closed}, nil
}

// Data type is resolved from raw config via ProviderRegistration.DataType, without constructing
// anything, so bounds wrapping must not build a throwaway primary instance just to inspect its
// type - only the one instance nested inside the crop wrapper should ever exist.
func Test_ConstructLayer_Bounds_ConstructsProviderOnce(t *testing.T) {
	RegisterProvider(wrapMarkerRegistration{name: "crop"})
	closed := false
	RegisterProvider(closableTypedTestRegistration{name: "closable-raster-1", dt: config.DataTypeRaster, closed: &closed})

	rawConfig := config.LayerConfig{
		ID:       "l10",
		Bounds:   config.BoundsConfig{South: -10, North: 10, West: -10, East: 10},
		Provider: map[string]any{"name": "closable-raster-1"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{InvalidParam: "invalid %v: %v", ParamRequired: "required %v"}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
	require.False(t, closed, "no provider should be closed when exactly one instance is ever constructed")
}
