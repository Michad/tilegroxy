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

package layers

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testErrorMessages = config.ErrorMessages{InvalidParam: "invalid %v: %v"}

func Test_ParsePattern(t *testing.T) {
	segments, err := parsePattern("hello")
	assert.Equal(t, []layerSegment{{value: "hello", placeholder: false}}, segments)
	require.NoError(t, err)

	segments, err = parsePattern("{hello}")
	assert.Equal(t, []layerSegment{{value: "hello", placeholder: true}}, segments)
	require.NoError(t, err)

	segments, err = parsePattern("{hello")
	assert.Equal(t, []layerSegment{{value: "hello", placeholder: true}}, segments)
	require.Error(t, err)

	segments, err = parsePattern("pre{hello}suf")
	assert.Equal(t, []layerSegment{
		{value: "pre", placeholder: false},
		{value: "hello", placeholder: true},
		{value: "suf", placeholder: false},
	}, segments)
	require.NoError(t, err)

	segments, err = parsePattern("a{b}c{d}e{f}")
	assert.Equal(t, []layerSegment{
		{value: "a", placeholder: false},
		{value: "b", placeholder: true},
		{value: "c", placeholder: false},
		{value: "d", placeholder: true},
		{value: "e", placeholder: false},
		{value: "f", placeholder: true},
	}, segments)
	require.NoError(t, err)

	segments, err = parsePattern("")
	assert.Equal(t, []layerSegment{}, segments)
	require.NoError(t, err)

	segments, err = parsePattern("}")
	assert.Equal(t, []layerSegment{{value: "}", placeholder: false}}, segments)
	require.NoError(t, err)

	_, err = parsePattern("a{b}c{d}e{d}")
	require.Error(t, err)

	_, err = parsePattern("a{b}{c}d")
	require.Error(t, err)

	_, err = parsePattern("{")
	require.Error(t, err)
}

func Test_MatchPattern(t *testing.T) {
	pattern := []layerSegment{
		{value: "a", placeholder: false},
		{value: "b", placeholder: true},
		{value: "c", placeholder: false},
		{value: "d", placeholder: true},
		{value: "e", placeholder: false},
		{value: "f", placeholder: true},
	}

	doesMatch, matches := match(pattern, "aHELLOcWORLDeTEST")
	assert.True(t, doesMatch)
	assert.Len(t, matches, 3)
	assert.Equal(t, "HELLO", matches["b"])
	assert.Equal(t, "WORLD", matches["d"])
	assert.Equal(t, "TEST", matches["f"])

	doesMatch, _ = match(pattern, "aNotContainingOthers_e")
	assert.False(t, doesMatch)

	doesMatch, matches = match(pattern, "aHELLOcWORLDe")
	assert.True(t, doesMatch)
	assert.Len(t, matches, 3)
	assert.Equal(t, "HELLO", matches["b"])
	assert.Equal(t, "WORLD", matches["d"])
	assert.Empty(t, matches["f"])

	pattern = []layerSegment{
		{value: "b", placeholder: true},
		{value: "c", placeholder: false},
		{value: "d", placeholder: true},
		{value: "e", placeholder: false},
		{value: "f", placeholder: true},
	}

	doesMatch, matches = match(pattern, "HELLOcWORLDeTEST")
	assert.True(t, doesMatch)
	assert.Len(t, matches, 3)
	assert.Equal(t, "HELLO", matches["b"])
	assert.Equal(t, "WORLD", matches["d"])
	assert.Equal(t, "TEST", matches["f"])

	pattern = []layerSegment{
		{value: "c", placeholder: false},
	}

	doesMatch, matches = match(pattern, "c")
	assert.True(t, doesMatch)
	assert.Empty(t, matches)

	doesMatch, matches = match(pattern, "ac")
	assert.False(t, doesMatch)
	assert.Empty(t, matches)

	doesMatch, matches = match(pattern, "ca")
	assert.False(t, doesMatch)
	assert.Empty(t, matches)
}

func Test_ValidateMatches(t *testing.T) {
	matches := make(map[string]string)
	rules := make(map[string]string)

	matches["test1"] = "allLetters"
	matches["test2"] = "letters4ndn4mb3r5"

	rules["*"] = "[a-zA-Z0-9]*"

	regex, err := constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["*"] = "^[a-zA-Z0-9]*$"

	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["*"] = "[a-zA-Z]*"
	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))

	delete(rules, "*")
	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	regex, err = constructValidation(nil, nil, testErrorMessages)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["test1"] = "[a-zA-Z]*"
	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["test1"] = "a"
	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))

	rules["test1"] = "[a-zA-Z]*"
	rules["test2"] = "[a-zA-Z0-9]*"
	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["*"] = "aaa"
	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))

	delete(rules, "*")
	rules["test3"] = ".+"
	regex, err = constructValidation(rules, nil, testErrorMessages)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))
}

func Test_ConstructValidation_EmptyExpression(t *testing.T) {
	require.NotPanics(t, func() {
		regex, err := constructValidation(map[string]string{"name": ""}, nil, testErrorMessages)
		require.Error(t, err)
		assert.Nil(t, regex)
		assert.Contains(t, err.Error(), "layer.paramValidator.name")
	})
}

func Test_ConstructValidation_AlternationIsFullyAnchored(t *testing.T) {
	for _, rule := range []string{"1[6-9][0-9]{2}|20[0-9]{2}", "^1[6-9][0-9]{2}|20[0-9]{2}$"} {
		regex, err := constructValidation(map[string]string{"year": rule}, nil, testErrorMessages)
		require.NoError(t, err)

		for _, year := range []string{"1860", "2050"} {
			assert.True(t, validateParamMatches(map[string]string{"year": year}, regex), rule+" "+year)
		}
		for _, year := range []string{"1860abc", "x2050", "x1860y", "1599"} {
			assert.False(t, validateParamMatches(map[string]string{"year": year}, regex), rule+" "+year)
		}
	}
}

func Test_ConstructValidation_MixedCasePlaceholder(t *testing.T) {
	// Lowercase to mirror what the config loader produces
	cfg := config.LayerConfig{ID: "a", Pattern: "a_{myYear}", ParamValidator: map[string]string{"myyear": "[0-9]{4}"}}
	matching, err := resolveMatching(cfg, testErrorMessages)
	require.NoError(t, err)

	doesMatch, matches := matchName(matching.segments, matching.validator, matching.paramMap, "a_2024")
	assert.True(t, doesMatch)
	assert.Equal(t, map[string]string{"myYear": "2024"}, matches)

	doesMatch, _ = matchName(matching.segments, matching.validator, matching.paramMap, "a_abcd")
	assert.False(t, doesMatch)
}

func Test_ValidateLayerPlaceholders(t *testing.T) {
	segments, err := parsePattern("tiles_{Region}_{year}")
	require.NoError(t, err)

	valid := map[string]any{
		"name":      "fallback",
		"primary":   map[string]any{"name": "proxy", "url": "https://example.com/{layer.Region}/{layer.year}/{z}"},
		"secondary": map[string]any{"name": "static", "color": "FFF"},
		"providers": []any{map[string]any{"name": "ref", "layer": "other_{layer.year}"}, 5},
	}
	require.NoError(t, validateLayerPlaceholders(valid, "layer.provider", segments, testErrorMessages))

	tests := map[string]struct {
		provider map[string]any
		path     string
	}{
		"typo":         {map[string]any{"url": "https://example.com/{layer.Regoin}"}, "layer.provider.url"},
		"case differs": {map[string]any{"url": "https://example.com/{layer.region}"}, "layer.provider.url"},
		"nested list":  {map[string]any{"providers": []any{map[string]any{"layer": "x_{layer.nope}"}}}, "layer.provider.providers[0].layer"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateLayerPlaceholders(tc.provider, "layer.provider", segments, testErrorMessages)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.path)
		})
	}
}

func Test_ValidateLayerPlaceholders_NoPattern(t *testing.T) {
	segments := []layerSegment{{value: "plain"}}

	err := validateLayerPlaceholders(map[string]any{"layer": "x_{layer.plain}"}, "layer.provider", segments, testErrorMessages)
	require.Error(t, err)
}
