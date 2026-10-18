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
)

func Test_ReplaceEnv_Nothing(t *testing.T) {
	raw := make(map[string]interface{})
	child := make(map[string]interface{})

	raw["H"] = "K"
	raw["f"] = 1.0
	raw["i"] = 1
	raw["a"] = []string{"a", "b", "c"}
	raw["child"] = child
	child["f"] = "saf"

	cloned := ReplaceEnv(raw)

	assert.Equal(t, raw, cloned)
}

func Test_ReplaceEnv_WithVals(t *testing.T) {
	t.Setenv("TEST", "val")
	t.Setenv("TEST2", "val2")
	raw := make(map[string]interface{})
	child := make(map[string]interface{})

	raw["H"] = "K"
	raw["f"] = 1.0
	raw["i"] = 1
	raw["a"] = []string{"a", "b", "c"}
	raw["child"] = child
	child["f"] = "saf"
	raw["p"] = "env.TEST"
	raw["fake"] = "env.FAKE"
	child["r"] = "env.TEST2"

	cloned := ReplaceEnv(raw)

	assert.Equal(t, "val", cloned["p"])
	assert.Empty(t, cloned["fake"])
	assert.Equal(t, "val2", cloned["child"].(map[string]interface{})["r"])
	assert.Equal(t, "saf", cloned["child"].(map[string]interface{})["f"])
}

// Values arrive as map[string]string, []interface{} and lists of maps, and placeholders in each must be substituted
func Test_ReplaceEnv_MapStringString(t *testing.T) {
	t.Setenv("TEST_HEADER", "secretvalue")

	raw := map[string]interface{}{
		"headers": map[string]string{
			"Authorization": "env.TEST_HEADER",
			"Other":         "literal",
		},
	}

	cloned := ReplaceEnv(raw)

	headers := cloned["headers"].(map[string]string)
	assert.Equal(t, "secretvalue", headers["Authorization"])
	assert.Equal(t, "literal", headers["Other"])
}

func Test_ReplaceEnv_ListOfInterface(t *testing.T) {
	t.Setenv("TEST_LIST_VAL", "fromenv")

	raw := map[string]interface{}{
		"list": []interface{}{"env.TEST_LIST_VAL", "literal"},
	}

	cloned := ReplaceEnv(raw)

	list := cloned["list"].([]interface{})
	assert.Equal(t, "fromenv", list[0])
	assert.Equal(t, "literal", list[1])
}

func Test_ReplaceEnv_ListOfMaps(t *testing.T) {
	t.Setenv("TEST_TIER_PATH", "/from/env")

	raw := map[string]interface{}{
		"tiers": []map[string]interface{}{
			{"path": "env.TEST_TIER_PATH"},
		},
	}

	cloned := ReplaceEnv(raw)

	tiers := cloned["tiers"].([]map[string]interface{})
	assert.Equal(t, "/from/env", tiers[0]["path"])
}

// A YAML key with no value (`ttl:`) is nil, which reflect turns into a zero Value that Convert panics on
func Test_ReplaceEnv_NilInMap(t *testing.T) {
	raw := map[string]interface{}{
		"ttl":   nil,
		"other": "literal",
	}

	cloned := ReplaceEnv(raw)

	assert.Nil(t, cloned["ttl"])
	assert.Equal(t, "literal", cloned["other"])
}

func Test_ReplaceEnv_NilInNestedMap(t *testing.T) {
	t.Setenv("TEST_NESTED_NIL", "fromenv")

	raw := map[string]interface{}{
		"cache": map[string]interface{}{
			"ttl":  nil,
			"path": "env.TEST_NESTED_NIL",
		},
	}

	cloned := ReplaceEnv(raw)

	cacheCfg := cloned["cache"].(map[string]interface{})
	assert.Nil(t, cacheCfg["ttl"])
	assert.Equal(t, "fromenv", cacheCfg["path"])
}

func Test_ReplaceEnv_NilInSlice(t *testing.T) {
	raw := map[string]interface{}{
		"list": []interface{}{nil, "literal"},
	}

	cloned := ReplaceEnv(raw)

	list := cloned["list"].([]interface{})
	assert.Nil(t, list[0])
	assert.Equal(t, "literal", list[1])
}

// The shape from the original crash report
func Test_ReplaceEnv_NilInsideListOfMaps(t *testing.T) {
	t.Setenv("TEST_TIER_NAME", "memory")

	raw := map[string]interface{}{
		"tiers": []interface{}{
			map[string]interface{}{"name": "env.TEST_TIER_NAME", "ttl": nil},
		},
	}

	cloned := ReplaceEnv(raw)

	tiers := cloned["tiers"].([]interface{})
	tier := tiers[0].(map[string]interface{})
	assert.Equal(t, "memory", tier["name"])
	assert.Nil(t, tier["ttl"])
}
