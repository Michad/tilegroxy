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

package tg

import (
	"bytes"
	"context"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

type testFixedProvider struct{}

func (testFixedProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (testFixedProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{Content: []byte{1, 2, 3}, ContentType: "image/png"}, nil
}

type testFixedRegistration struct{}

func (testFixedRegistration) Name() string                   { return "test-fixed-provider" }
func (testFixedRegistration) InitializeConfig() any          { return struct{}{} }
func (testFixedRegistration) DataType(_ any) config.DataType { return config.DataTypeRaster }
func (testFixedRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return testFixedProvider{}, nil
}

// A pattern layer has no single concrete name, so testing "all layers" (or naming it explicitly)
// must expand it into its configured examples rather than testing its bare id/pattern.
func Test_Test_ExpandsPatternLayerIntoExamples(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:      "pattern_layer",
			Pattern: "my_{name}_{version}",
			Examples: []string{
				"my_foo_v1",
				"my_bar_v2",
			},
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "my_foo_v1")
	require.Contains(t, out.String(), "my_bar_v2")
	require.NotContains(t, out.String(), "pattern_layer")
}

// A pattern layer without any configured examples has nothing concrete to test, so it's skipped
// with a warning instead of failing the whole run or testing its meaningless bare id/pattern.
func Test_Test_SkipsPatternLayerWithNoExamples(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "pattern_layer",
			Pattern:  "my_{name}_{version}",
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
}

// A plain id layer keeps being tested under its own name, unaffected by pattern expansion.
func Test_Test_PlainLayerUnaffected(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "plain_layer",
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "plain_layer")
}

// An explicitly requested name is tested as given, so a pattern layer can be tested against a
// name that isn't one of its examples.
func Test_Test_ExplicitNameUsedAsGiven(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "pattern_layer",
			Pattern:  "my_{name}_{version}",
			Examples: []string{"my_foo_v1"},
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{LayerNames: []string{"my_other_v9"}, Z: 1, X: 0, Y: 0, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "my_other_v9")
	require.NotContains(t, out.String(), "my_foo_v1")
}
