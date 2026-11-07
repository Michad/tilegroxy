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

package layers

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func floodLayerConfig() config.LayerConfig {
	return config.LayerConfig{
		ID:             "flood",
		Pattern:        "flood_{floodType}_{period}",
		ParamValidator: map[string]string{"period": "[0-9]{6}"},
		ParamMap: map[string][]config.ParamMapEntry{
			// Lowercase to mirror what the config loader produces
			"floodtype": {
				{From: "river", To: "RiverFlood_v2"},
				{From: "Coastal", To: "CoastalFlood_v3"},
				{From: "sea", To: "CoastalFlood_v3"},
			},
		},
		Provider: map[string]any{"name": "sample-provider"},
	}
}

func Test_ParamMap_MatchesNameReplacesValue(t *testing.T) {
	layer.RegisterProvider(sampleProviderRegistration{})

	l, err := ConstructLayer(pkg.BackgroundContext(), floodLayerConfig(), config.ClientConfig{}, nil, testErrorMessages, nil, nil, nil, nil)
	require.NoError(t, err)

	tests := []struct {
		name      string
		layerName string
		want      string
	}{
		{"mapped", "flood_river_202401", "RiverFlood_v2"},
		{"case preserved in from", "flood_Coastal_202401", "CoastalFlood_v3"},
		{"several to one", "flood_sea_202401", "CoastalFlood_v3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := pkg.BackgroundContext()
			require.True(t, l.MatchesName(ctx, tt.layerName))

			lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
			assert.Equal(t, map[string]string{"floodType": tt.want, "period": "202401"}, *lpm)
		})
	}
}

func Test_ParamMap_UnlistedValueDoesNotMatch(t *testing.T) {
	cfg := floodLayerConfig()

	for _, name := range []string{"flood_coastal_202401", "flood_lake_202401", "flood_river_2024", "flood_RiverFlood_v2_202401"} {
		assert.False(t, ConfigMatchesName(cfg, testErrorMessages, name), name)
	}
	assert.True(t, ConfigMatchesName(cfg, testErrorMessages, "flood_sea_202401"))
}

func Test_ParamMap_ExamplesAreChecked(t *testing.T) {
	cfg := floodLayerConfig()
	cfg.Examples = []string{"flood_sea_202401"}
	_, err := resolveMatching(cfg, testErrorMessages)
	require.NoError(t, err)

	cfg.Examples = []string{"flood_lake_202401"}
	_, err = resolveMatching(cfg, testErrorMessages)
	require.ErrorContains(t, err, "layer.examples")
}

func Test_ParamMap_InvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.LayerConfig
		contains string
	}{
		{
			name: "unknown placeholder",
			cfg: config.LayerConfig{ID: "a", Pattern: "a_{x}", ParamMap: map[string][]config.ParamMapEntry{
				"y": {{From: "1", To: "2"}},
			}},
			contains: "layer.paramMap: y",
		},
		{
			name: "no pattern",
			cfg: config.LayerConfig{ID: "a", ParamMap: map[string][]config.ParamMapEntry{
				"a": {{From: "1", To: "2"}},
			}},
			contains: "layer.paramMap: a",
		},
		{
			name: "duplicate from",
			cfg: config.LayerConfig{ID: "a", Pattern: "a_{x}", ParamMap: map[string][]config.ParamMapEntry{
				"x": {{From: "1", To: "2"}, {From: "1", To: "3"}},
			}},
			contains: "layer.paramMap.x.from: 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveMatching(tt.cfg, testErrorMessages)
			require.ErrorContains(t, err, tt.contains)
			assert.False(t, ConfigMatchesName(tt.cfg, testErrorMessages, "a_1"))
		})
	}
}

func Test_ParamMap_Default(t *testing.T) {
	tests := []struct {
		name         string
		defaultTo    string
		unlisted     string
		wantUnlisted string
	}{
		{"passes through", "", "1999", "1999"},
		{"replaces", "1900", "1999", "1900"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matching, err := resolveMatching(config.LayerConfig{ID: "a", Pattern: "a_{version}", ParamMap: map[string][]config.ParamMapEntry{
				"version": {{From: "2012", To: "2012a"}, {Default: true, To: tt.defaultTo}},
			}}, testErrorMessages)
			require.NoError(t, err)

			ok, matches := matchName(matching.segments, matching.validator, matching.paramMap, "a_2012")
			require.True(t, ok)
			assert.Equal(t, "2012a", matches["version"])

			ok, matches = matchName(matching.segments, matching.validator, matching.paramMap, "a_"+tt.unlisted)
			require.True(t, ok)
			assert.Equal(t, tt.wantUnlisted, matches["version"])
		})
	}
}

func Test_ParamMap_InvalidDefault(t *testing.T) {
	tests := []struct {
		name    string
		entries []config.ParamMapEntry
	}{
		{"two defaults", []config.ParamMapEntry{{Default: true}, {Default: true, To: "x"}}},
		{"default with from", []config.ParamMapEntry{{Default: true, From: "1"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveMatching(config.LayerConfig{ID: "a", Pattern: "a_{x}", ParamMap: map[string][]config.ParamMapEntry{"x": tt.entries}}, testErrorMessages)
			require.ErrorContains(t, err, "layer.paramMap.x.default")
		})
	}
}

func Test_UnprotectedPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.LayerConfig
		want []string
	}{
		{"no pattern", config.LayerConfig{ID: "a"}, []string{}},
		{"nothing", config.LayerConfig{ID: "a", Pattern: "a_{x}_{y}"}, []string{"x", "y"}},
		{"wildcard validator", config.LayerConfig{ID: "a", Pattern: "a_{x}_{y}", ParamValidator: map[string]string{"*": ".+"}}, []string{}},
		{"validator ignores case", config.LayerConfig{ID: "a", Pattern: "a_{myX}_{y}", ParamValidator: map[string]string{"myx": ".+"}}, []string{"y"}},
		{"map", config.LayerConfig{ID: "a", Pattern: "a_{x}", ParamMap: map[string][]config.ParamMapEntry{
			"x": {{From: "1", To: "2"}},
		}}, []string{}},
		{"map with replacing default", config.LayerConfig{ID: "a", Pattern: "a_{x}", ParamMap: map[string][]config.ParamMapEntry{
			"x": {{From: "1", To: "2"}, {Default: true, To: "3"}},
		}}, []string{}},
		{"map with passthrough default", config.LayerConfig{ID: "a", Pattern: "a_{x}", ParamMap: map[string][]config.ParamMapEntry{
			"x": {{From: "1", To: "2"}, {Default: true}},
		}}, []string{"x"}},
		{"passthrough default with validator", config.LayerConfig{ID: "a", Pattern: "a_{x}", ParamValidator: map[string]string{"x": "[0-9]"}, ParamMap: map[string][]config.ParamMapEntry{
			"x": {{Default: true}},
		}}, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matching, err := resolveMatching(tt.cfg, testErrorMessages)
			require.NoError(t, err)
			assert.Equal(t, tt.want, unprotectedPlaceholders(matching))
		})
	}
}

func Test_ConstructLayer_WarnsOnUnprotectedPlaceholder(t *testing.T) {
	layer.RegisterProvider(sampleProviderRegistration{})

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := config.LayerConfig{ID: "open", Pattern: "open_{x}", Provider: map[string]any{"name": "sample-provider"}}
	_, err := ConstructLayer(pkg.BackgroundContext(), cfg, config.ClientConfig{}, nil, testErrorMessages, nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Layer open accepts any value for placeholders [x]")

	buf.Reset()
	_, err = ConstructLayer(pkg.BackgroundContext(), floodLayerConfig(), config.ClientConfig{}, nil, testErrorMessages, nil, nil, nil, nil)
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "accepts any value")
}
