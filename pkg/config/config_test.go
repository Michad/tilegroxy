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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleYml(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/simple.yml")

	require.NoError(t, err)
	assert.Equal(t, "none", c.Cache["name"])
}

func TestSimpleJson(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/simple.json")

	require.NoError(t, err)
	assert.Equal(t, "none", c.Cache["name"])
}

func TestComplexYml(t *testing.T) {
	_, err := LoadConfigFromFile("../../examples/configurations/complex.yml")

	require.NoError(t, err)
}

func TestTwoTierYml(t *testing.T) {
	_, err := LoadConfigFromFile("../../examples/configurations/two_tier_cache.yml")

	require.NoError(t, err)
}

func TestMergeDefaultsFrom(t *testing.T) {
	c1 := DefaultConfig().Client

	var c2 ClientConfig

	c2.MergeDefaultsFrom(c1)

	assert.Equal(t, c1.ContentTypes, c2.ContentTypes)
	assert.Equal(t, c1.Headers, c2.Headers)
	assert.Equal(t, c1.MaxLength, c2.MaxLength)
	assert.Equal(t, c1.RewriteContentTypes, c2.RewriteContentTypes)
	assert.Equal(t, c1.StatusCodes, c2.StatusCodes)
	assert.Equal(t, c1.Timeout, c2.Timeout)
	assert.Equal(t, c1.UnknownLength, c2.UnknownLength)
	assert.Equal(t, c1.UserAgent, c2.UserAgent)

	var c3 ClientConfig
	c3.Headers = map[string]string{"test": "test"}

	c3.MergeDefaultsFrom(c1)

	assert.Equal(t, c1.ContentTypes, c3.ContentTypes)
	assert.NotEqual(t, c1.Headers, c3.Headers)
	assert.Equal(t, c1.MaxLength, c3.MaxLength)
	assert.Equal(t, c1.RewriteContentTypes, c3.RewriteContentTypes)
	assert.Equal(t, c1.StatusCodes, c3.StatusCodes)
	assert.Equal(t, c1.Timeout, c3.Timeout)
	assert.Equal(t, c1.UnknownLength, c3.UnknownLength)
	assert.Equal(t, c1.UserAgent, c3.UserAgent)
}
