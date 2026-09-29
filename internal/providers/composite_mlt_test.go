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
	"testing"

	"github.com/Michad/tilegroxy/internal/mlt"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_CompositeMLT(t *testing.T) {
	assert.Equal(t, config.DataTypeMLT, CompositeMLTRegistration{}.DataType(CompositeMLTConfig{}))
}

// MLT tiles are a run of layers, so joining two tiles gives one tile holding both.
func Test_CompositeMLT_ExecuteStatic(t *testing.T) {
	child := map[string]interface{}{"name": "static", "image": "embedded:box.mlt"}

	c, err := CompositeMLTRegistration{}.Initialize(CompositeMLTConfig{Providers: []map[string]interface{}{child, child}}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	require.NoError(t, err)

	img, err := c.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})
	require.NoError(t, err)
	assert.Equal(t, "application/vnd.maplibre-tile", img.ContentType)

	layers, skipped, err := mlt.Decode(img.Content)
	require.NoError(t, err)
	assert.Zero(t, skipped)
	assert.Len(t, layers, 2)
}

func Test_Composite_RejectsOtherVectorFormat(t *testing.T) {
	deps := layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages}
	mvtChild := map[string]interface{}{"name": "static", "image": "embedded:box.mvt"}
	mltChild := map[string]interface{}{"name": "static", "image": "embedded:box.mlt"}

	_, err := CompositeMLTRegistration{}.Initialize(CompositeMLTConfig{Providers: []map[string]interface{}{mltChild, mvtChild}}, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider.compositemlt.providers.1")

	_, err = CompositeMVTRegistration{}.Initialize(CompositeMVTConfig{Providers: []map[string]interface{}{mltChild}}, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider.compositemvt.providers.0")

	// A child of unknown type, such as a proxy, may produce either format.
	proxy := map[string]interface{}{"name": "proxy", "url": "http://example.com/{z}/{x}/{y}"}
	_, err = CompositeMLTRegistration{}.Initialize(CompositeMLTConfig{Providers: []map[string]interface{}{proxy}}, deps)
	require.NoError(t, err)
}

// Operators configure providers as maps, so the registrations must build from one.
func Test_MLTProviders_ConstructFromConfig(t *testing.T) {
	deps := layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages}
	child := map[string]interface{}{"name": "static", "image": "embedded:box.mlt"}

	composite, err := layer.ConstructProvider(map[string]interface{}{"name": "compositemlt", "providers": []map[string]interface{}{child}}, deps)
	require.NoError(t, err)
	assert.Equal(t, config.DataTypeMLT, layer.DescribeTree(composite).DataType)

	crop, err := layer.ConstructProvider(map[string]interface{}{"name": "cropmlt", "primary": child, "bounds": map[string]interface{}{"north": 10, "south": -10, "east": 10, "west": -10}}, deps)
	require.NoError(t, err)
	assert.Equal(t, config.DataTypeMLT, layer.DescribeTree(crop).DataType)
}
