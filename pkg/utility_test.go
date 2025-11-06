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
package pkg

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseZoom(t *testing.T) {
	zooms, err := ParseZoomString("1")
	assert.Equal(t, []int{1}, zooms)
	require.NoError(t, err)

	zooms, err = ParseZoomString("1-2")
	assert.Equal(t, []int{1, 2}, zooms)
	require.NoError(t, err)

	zooms, err = ParseZoomString("1,2")
	assert.Equal(t, []int{1, 2}, zooms)
	require.NoError(t, err)

	_, err = ParseZoomString("2-1")
	require.Error(t, err)

	_, err = ParseZoomString("fish")
	require.Error(t, err)

	_, err = ParseZoomString("f")
	require.Error(t, err)

	_, err = ParseZoomString("-1")
	require.Error(t, err)

	_, err = ParseZoomString("25")
	require.Error(t, err)

	_, err = ParseZoomString("2-30")
	require.Error(t, err)

	_, err = ParseZoomString("-1-1")
	require.Error(t, err)
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

func Test_Ternary(t *testing.T) {
	assert.Equal(t, "a", Ternary(true, "a", "b"))
	assert.Equal(t, "b", Ternary(false, "a", "b"))
}

func Fuzz_EncodeDecodeImage(f *testing.F) {
	for z := 1; z < 100; z++ {
		b := make([]byte, rand.IntN(1000))

		for i := range b {
			b[i] = byte(rand.UintN(255))
		}

		c := RandomString() + "/" + RandomString()

		f.Add(b, c)
	}
	f.Fuzz(func(t *testing.T, b []byte, c string) {
		img1 := Image{Content: b, ContentType: c}

		b, err := img1.Encode()
		require.NoError(t, err)
		img2, err := DecodeImage(b)
		require.NoError(t, err)

		assert.Equal(t, img1.ContentType, img2.ContentType)
		assert.Equal(t, img1.Content, img2.Content)

		// Test backwards compatibility
		img3, err := DecodeImage(img1.Content)
		require.NoError(t, err)
		assert.Equal(t, img3.Content, img2.Content)
		assert.Empty(t, img3.ContentType)
	})
}
