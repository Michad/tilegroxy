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
	"context"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
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

// Config values legitimately arrive as map[string]string, []interface{}, and lists of maps, not
// only map[string]interface{}, and a placeholder in any of them has to be substituted.
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

// A YAML key written with no value (`ttl:`) parses to a nil, which reflect.ValueOf turns into the
// zero Value that Convert panics on. Nils must pass through untouched.
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

// The shape from the original crash report: a list of maps where one map value is nil.
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

func Test_Ternary(t *testing.T) {
	assert.Equal(t, "a", Ternary(true, "a", "b"))
	assert.Equal(t, "b", Ternary(false, "a", "b"))
}

// Provider URLs are templated and a {ctx.*} placeholder resolves to a value off the incoming
// request, so a debug log of the outgoing URL could carry a live credential.
func Test_RedactURLForLog(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary url untouched", "https://example.com/1/2/3.png", "https://example.com/1/2/3.png"},
		{"non-credential params kept", "https://example.com/t?z=1&x=2", "https://example.com/t?x=2&z=1"},
		{"api key masked", "https://example.com/t?key=abc123", "https://example.com/t?key=redacted"},
		{"access token masked", "https://example.com/t?access_token=abc123", "https://example.com/t?access_token=redacted"},
		{"param name case insensitive", "https://example.com/t?ApiKey=abc123", "https://example.com/t?ApiKey=redacted"},
		{"userinfo masked", "https://user:pass@example.com/t", "https://redacted@example.com/t"},
		// The CGI provider logs a relative URI rather than an absolute URL.
		{"relative uri untouched", "/cgi-bin/mapserv?z=1", "/cgi-bin/mapserv?z=1"},
		{"relative uri key masked", "/cgi-bin/mapserv?key=abc123", "/cgi-bin/mapserv?key=redacted"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RedactURLForLog(c.in))
		})
	}

	assert.NotContains(t, RedactURLForLog("https://example.com/t?token=supersecret"), "supersecret")
	assert.Equal(t, "(unparseable url)", RedactURLForLog("http://[::1]bad:99/"))
}

// GetTile is exported from pkg so a library consumer writing their own Go provider gets the same
// MaxLength/ContentTypes/StatusCodes enforcement as the built-in ones without reimplementing it.
func Test_GetTile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tiledata"))
	}))
	defer server.Close()

	clientConfig := config.ClientConfig{
		StatusCodes:  []int{http.StatusOK},
		ContentTypes: []string{"image/png"},
		MaxLength:    1024,
		Timeout:      5,
	}

	img, err := GetTile(context.Background(), clientConfig, server.URL, nil)

	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, []byte("tiledata"), img.Content)
	assert.Equal(t, "image/png", img.ContentType)
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
