// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ResolveEntityKeys_ResolvesNameAndID(t *testing.T) {
	t.Setenv("TEST_ENTITY_NAME", "proxy")
	t.Setenv("TEST_ENTITY_ID", "main")

	raw := map[string]interface{}{
		"name":  "env.TEST_ENTITY_NAME",
		"ID":    "{env.TEST_ENTITY_ID}-1",
		"url":   "env.TEST_ENTITY_NAME",
		"child": map[string]interface{}{"name": "env.TEST_ENTITY_NAME"},
	}

	resolved, err := ResolveEntityKeys(raw, nil)

	require.NoError(t, err)
	assert.Equal(t, "proxy", resolved["name"])
	assert.Equal(t, "main-1", resolved["ID"])
	assert.Equal(t, "env.TEST_ENTITY_NAME", resolved["url"])
	assert.Equal(t, "env.TEST_ENTITY_NAME", resolved["child"].(map[string]interface{})["name"])
	assert.Equal(t, "env.TEST_ENTITY_NAME", raw["name"])
}

func Test_ResolveEntityKeys_Nil(t *testing.T) {
	resolved, err := ResolveEntityKeys(nil, nil)

	require.NoError(t, err)
	assert.Nil(t, resolved)
}

func Test_ResolveEntityKeys_SecretWithoutStore(t *testing.T) {
	_, err := ResolveEntityKeys(map[string]interface{}{"name": "secret.foo"}, nil)

	require.Error(t, err)
}
