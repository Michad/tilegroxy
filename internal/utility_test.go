// Copyright 2024 Michael Davis
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
package internal

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseZoom(t *testing.T) {
	zooms, err := ParseZoomString("1")
	assert.Equal(t, []int{1}, zooms)
	assert.NoError(t, err)

	zooms, err = ParseZoomString("1-2")
	assert.Equal(t, []int{1, 2}, zooms)
	assert.NoError(t, err)

	zooms, err = ParseZoomString("1,2")
	assert.Equal(t, []int{1, 2}, zooms)
	assert.NoError(t, err)

	_, err = ParseZoomString("2-1")
	assert.Error(t, err)

	_, err = ParseZoomString("fish")
	assert.Error(t, err)

	_, err = ParseZoomString("f")
	assert.Error(t, err)

	_, err = ParseZoomString("-1")
	assert.Error(t, err)

	_, err = ParseZoomString("25")
	assert.Error(t, err)

	_, err = ParseZoomString("2-30")
	assert.Error(t, err)

	_, err = ParseZoomString("-1-1")
	assert.Error(t, err)
}

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
	os.Setenv("TEST", "val")
	os.Setenv("TEST2", "val2")
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
	assert.Equal(t, "", cloned["fake"])
	assert.Equal(t, "val2", cloned["child"].(map[string]interface{})["r"])
	assert.Equal(t, "saf", cloned["child"].(map[string]interface{})["f"])
}
