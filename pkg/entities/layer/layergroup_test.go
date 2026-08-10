// Copyright 2024 Michael Davis
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
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

func refProvider(target string) map[string]any {
	return map[string]any{"name": "ref", "layer": target}
}

func staticProvider() map[string]any {
	return map[string]any{"name": "static", "color": "FFF"}
}

// An unsanitized layer ID with a space produces an invalid OTEL instrument name, failing
// Int64Counter construction, which is fatal at startup and silent on hot reload.
func Test_ConstructLayerGroup_LayerIDWithSpaceDoesNotFailConstruction(t *testing.T) {
	RegisterProvider(docExampleSampleRegistration{})

	layers := []config.LayerConfig{
		{ID: "my layer", Provider: map[string]any{"name": "doc-example-sample"}},
	}

	lg, err := ConstructLayerGroup(config.Config{Layers: layers}, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, lg)
}

func Test_ConstructLayerGroup_LayerIDWithNonASCIIDoesNotFailConstruction(t *testing.T) {
	RegisterProvider(docExampleSampleRegistration{})

	layers := []config.LayerConfig{
		{ID: "层", Provider: map[string]any{"name": "doc-example-sample"}},
	}

	lg, err := ConstructLayerGroup(config.Config{Layers: layers}, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, lg)
}

func Test_ConstructLayerGroup_DuplicateLayerIDErrors(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "dupe", Provider: staticProvider()},
		{ID: "dupe", Provider: staticProvider()},
	}

	_, err := ConstructLayerGroup(config.Config{Layers: layers}, nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate layer id")
}

func Test_ValidateRefs_DirectCycle(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("b")},
		{ID: "b", Provider: refProvider("a")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func Test_ValidateRefs_SelfCycle(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("a")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func Test_ValidateRefs_CycleNestedInBlend(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: map[string]any{
			"name": "blend",
			"providers": []any{
				refProvider("b"),
				staticProvider(),
			},
		}},
		{ID: "b", Provider: refProvider("a")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func Test_ValidateRefs_DanglingTarget(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("nonexistent")},
	}

	err := validateRefs(layers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown layer")
}

func Test_ValidateRefs_DanglingTargetSkippedWithPatternLayers(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("maybe_pattern_match")},
		{ID: "b", Pattern: "pattern_{x}", Provider: staticProvider()},
	}

	// Can't statically prove "maybe_pattern_match" doesn't match the pattern layer, so no error
	err := validateRefs(layers)
	require.NoError(t, err)
}

func Test_ValidateRefs_ValidChain(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: refProvider("b")},
		{ID: "b", Provider: refProvider("c")},
		{ID: "c", Provider: staticProvider()},
	}

	err := validateRefs(layers)
	require.NoError(t, err)
}

func Test_ValidateRefs_NoRefs(t *testing.T) {
	layers := []config.LayerConfig{
		{ID: "a", Provider: staticProvider()},
	}

	err := validateRefs(layers)
	require.NoError(t, err)
}
