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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, "", matches["f"])

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

	regex, err := constructValidation(rules)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["*"] = "^[a-zA-Z0-9]*$"

	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["*"] = "[a-zA-Z]*"
	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))

	delete(rules, "*")
	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	regex, err = constructValidation(nil)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["test1"] = "[a-zA-Z]*"
	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["test1"] = "a"
	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))

	rules["test1"] = "[a-zA-Z]*"
	rules["test2"] = "[a-zA-Z0-9]*"
	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.True(t, validateParamMatches(matches, regex))

	rules["*"] = "aaa"
	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))

	delete(rules, "*")
	rules["test3"] = ".+"
	regex, err = constructValidation(rules)
	require.NoError(t, err)
	assert.False(t, validateParamMatches(matches, regex))
}
