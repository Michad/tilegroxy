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

package placeholder

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testVars = TileVars{Z: 3, X: 2, Y: 1, Bounds: pkg.Bounds{South: -1.5, West: -2.5, North: 1.5, East: 2.5}, LayerName: "test"}

func requestContext(t *testing.T, target string, header http.Header) context.Context {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	require.NoError(t, err)

	for k, v := range header {
		req.Header[k] = v
	}

	ctx := pkg.NewRequestContext(req)

	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "matched"

	tenant, _ := pkg.TenantIDFromContext(ctx)
	*tenant = "acme"

	return ctx
}

func render(ctx context.Context, t *testing.T, str string) string {
	t.Helper()

	tmpl, err := ParseTemplate(str, secretResolver())
	require.NoError(t, err)

	var sink RawSink
	tmpl.Render(ctx, testVars, &sink)

	return sink.String()
}

func Test_Render_Sources(t *testing.T) {
	t.Setenv("TEST_TEMPLATE_A", "a")

	ctx := requestContext(t, "http://example.com/tiles/x?q=1&q=2&single=s", http.Header{
		"X-Trace":  []string{"one", "two"},
		"X-Single": []string{"s"},
	})

	tests := map[string]string{
		"{z}/{x}/{y}/{Z}/{X}/{Y}":         "3/2/1/3/2/1",
		"{xmin},{ymin},{xmax},{ymax}":     "-2.500000,-1.500000,2.500000,1.500000",
		"{env.TEST_TEMPLATE_A}":           "a",
		"{secret.pw}":                     "hunter2",
		"{ctx.tenant}":                    "acme",
		"{ctx.user}":                      "",
		"{ctx.method}":                    "GET",
		"{ctx.X-Single}":                  "s",
		"{ctx.X-Trace}":                   "one,two",
		"{hdr.x-single}":                  "s",
		"{hdr.X-Trace}":                   "one,two",
		"{query.q}":                       "1,2",
		"{query.single}":                  "s",
		"{layer.v}":                       "matched",
		"{hdr.missing:-dflt}":             "dflt",
		"{query.missing:-dflt}":           "dflt",
		"{layer.missing:-dflt}":           "dflt",
		"{ctx.missing:-dflt}":             "dflt",
		"{ctx.missing}|{hdr.missing}|":    "||",
		"{env.TEST_TEMPLATE_UNSET:-e}":    "e",
		"{env.TEST_TEMPLATE_UNSET}":       "",
		"{secret.missing:-fallback}":      "fallback",
		`'{"json":true}' AND {z}`:         `'{"json":true}' AND 3`,
		"{query.single}{query.missing:-}": "s",
	}

	for in, expected := range tests {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, expected, render(ctx, t, in))
		})
	}
}

// seed and test build requests from a background context
func Test_Render_BackgroundContext(t *testing.T) {
	ctx := pkg.BackgroundContext()

	assert.Equal(t, "|dflt|", render(ctx, t, "{hdr.X-Any}|{query.q:-dflt}|{layer.v}"))
	assert.Empty(t, render(context.Background(), t, "{hdr.X-Any}{query.q}{layer.v}{ctx.user}"))
}

// A whole value supplies the template, so its placeholders still work
func Test_ParseTemplate_WholeValue(t *testing.T) {
	t.Setenv("TEST_TEMPLATE_URL", "https://example.com/{z}/{x}/{y}.png")

	assert.Equal(t, "https://example.com/3/2/1.png", render(context.Background(), t, "env.TEST_TEMPLATE_URL"))

	tmpl, err := ParseTemplate("secret.pw", secretResolver())
	require.NoError(t, err)
	assert.Equal(t, "secret.pw", tmpl.String())

	_, err = ParseTemplate("env.TEST_TEMPLATE_URL", nil)
	require.NoError(t, err)

	_, err = ParseTemplate("secret.pw", nil)
	require.Error(t, err)
}

// An embedded value is inserted as-is, so a request can't be steered by placeholders hidden in it
func Test_ParseTemplate_EmbeddedValueNotRescanned(t *testing.T) {
	t.Setenv("TEST_TEMPLATE_BRACES", "{z}{ctx.tenant}")

	assert.Equal(t, "{z}{ctx.tenant}", render(requestContext(t, "http://a/", nil), t, "{env.TEST_TEMPLATE_BRACES}"))
}

func Test_Template_UnmarshalTextLeavesStaticsUnresolved(t *testing.T) {
	t.Setenv("TEST_TEMPLATE_A", "a")

	var tmpl Template
	require.NoError(t, tmpl.UnmarshalText([]byte("{env.TEST_TEMPLATE_A}/{z}")))

	var sink SQLSink
	tmpl.Render(context.Background(), testVars, &sink)

	assert.Equal(t, "{env.TEST_TEMPLATE_A}/{z}", tmpl.String())
	assert.False(t, tmpl.IsEmpty())
	assert.Equal(t, []any{nil}, sink.Params())
}

func Test_Template_Empty(t *testing.T) {
	var tmpl Template

	assert.True(t, tmpl.IsEmpty())

	var sink RawSink
	tmpl.Render(context.Background(), testVars, &sink)
	assert.Empty(t, sink.String())
}

func Test_Template_MapLiterals(t *testing.T) {
	t.Setenv("TEST_TEMPLATE_DOLLAR", "$zoom")

	tmpl, err := ParseTemplate("https://e.com/{env.TEST_TEMPLATE_DOLLAR}?z=$zoom&w=$width", nil)
	require.NoError(t, err)

	tmpl = tmpl.MapLiterals(strings.NewReplacer("$zoom", "{z}", "$width", "256").Replace)

	var sink RawSink
	tmpl.Render(context.Background(), testVars, &sink)

	assert.Equal(t, "https://e.com/$zoom?z=3&w=256", sink.String())
}

func Test_Template_Uses(t *testing.T) {
	tmpl, err := ParseTemplate("{z}/{layer.v}", nil)
	require.NoError(t, err)

	assert.True(t, tmpl.Uses(SourceLayer))
	assert.True(t, tmpl.Uses(SourceTile))
	assert.False(t, tmpl.Uses(SourceCtx))
}
