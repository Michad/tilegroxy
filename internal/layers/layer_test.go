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

	"github.com/stretchr/testify/assert"
)

func Test_ParsePattern(t *testing.T) {
	segments, err := parsePattern("hello")
	assert.Equal(t, []layerSegment{{value: "hello", placeholder: false}}, segments)
	assert.NoError(t, err)

	segments, err = parsePattern("{hello}")
	assert.Equal(t, []layerSegment{{value: "hello", placeholder: true}}, segments)
	assert.NoError(t, err)

	segments, err = parsePattern("{hello")
	assert.Equal(t, []layerSegment{{value: "hello", placeholder: true}}, segments)
	assert.Error(t, err)

	segments, err = parsePattern("pre{hello}suf")
	assert.Equal(t, []layerSegment{
		{value: "pre", placeholder: false},
		{value: "hello", placeholder: true},
		{value: "suf", placeholder: false},
	}, segments)
	assert.NoError(t, err)

	segments, err = parsePattern("a{b}c{d}e{f}")
	assert.Equal(t, []layerSegment{
		{value: "a", placeholder: false},
		{value: "b", placeholder: true},
		{value: "c", placeholder: false},
		{value: "d", placeholder: true},
		{value: "e", placeholder: false},
		{value: "f", placeholder: true},
	}, segments)
	assert.NoError(t, err)

	segments, err = parsePattern("")
	assert.Equal(t, []layerSegment{}, segments)
	assert.NoError(t, err)

	segments, err = parsePattern("}")
	assert.Equal(t, []layerSegment{{value: "}", placeholder: false}}, segments)
	assert.NoError(t, err)

	_, err = parsePattern("a{b}c{d}e{d}")
	assert.Error(t, err)

	_, err = parsePattern("a{b}{c}d")
	assert.Error(t, err)

	_, err = parsePattern("{")
	assert.Error(t, err)
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
	assert.Equal(t, 3, len(matches))
	assert.Equal(t, "HELLO", matches["b"])
	assert.Equal(t, "WORLD", matches["d"])
	assert.Equal(t, "TEST", matches["f"])

	doesMatch, _ = match(pattern, "aNotContainingOthers_e")
	assert.False(t, doesMatch)

	doesMatch, matches = match(pattern, "aHELLOcWORLDe")
	assert.True(t, doesMatch)
	assert.Equal(t, 3, len(matches))
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
	assert.Equal(t, 3, len(matches))
	assert.Equal(t, "HELLO", matches["b"])
	assert.Equal(t, "WORLD", matches["d"])
	assert.Equal(t, "TEST", matches["f"])
}
