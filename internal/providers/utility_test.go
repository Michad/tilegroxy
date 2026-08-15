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

package providers

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/require"
)

// A {layer.*} value comes from the request path via pattern matching, so unescaped it could
// append query parameters, truncate the path, or traverse it.
func Test_ReplaceURLPlaceholders_EscapesInjectionInPath(t *testing.T) {
	ctx := pkg.BackgroundContext()
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "evil?extra=param"

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles/{layer.v}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "?extra=param")
	require.Contains(t, result, "evil%3Fextra=param")
}

func Test_ReplaceURLPlaceholders_EscapesPathTraversal(t *testing.T) {
	ctx := pkg.BackgroundContext()
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "../../escaped"

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles/{layer.v}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "/../../escaped/")
	require.Contains(t, result, "..%2F..%2Fescaped")
}

func Test_ReplaceURLPlaceholders_EscapesFragmentInjection(t *testing.T) {
	ctx := pkg.BackgroundContext()
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "evil#fragment"

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles/{layer.v}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "#fragment")
	require.Contains(t, result, "evil%23fragment")
}

func Test_ReplaceURLPlaceholders_EscapesInQueryPosition(t *testing.T) {
	ctx := pkg.BackgroundContext()
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "value&injected=1"

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles?layer={layer.v}&z={z}", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "&injected=1")
	require.Contains(t, result, "value%26injected%3D1")
}

func Test_ReplaceURLPlaceholders_NormalValuesStillWork(t *testing.T) {
	ctx := pkg.BackgroundContext()
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "20230917a"

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles/{layer.v}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/tiles/20230917a/1/1/0.png", result)
}

// {env.*} is operator config, and the documented idiom is to use it for an entire base URL, so
// escaping it would mangle the "://" and "/" into "https:%2F%2Ftiles.example.com%2Fv1".
func Test_ReplaceURLPlaceholders_EnvValuePassesThroughUnescaped(t *testing.T) {
	t.Setenv("TILEGROXY_TEST_BASEURL", "https://tiles.example.com/v1")

	ctx := pkg.BackgroundContext()

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "{env.TILEGROXY_TEST_BASEURL}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.Equal(t, "https://tiles.example.com/v1/1/1/0.png", result)
}

// Operators may inject a full URL including query parameters, which must also survive intact.
func Test_ReplaceURLPlaceholders_EnvValueWithQueryStringPassesThroughUnescaped(t *testing.T) {
	t.Setenv("TILEGROXY_TEST_SUFFIX", "?key=abc&fmt=png")

	ctx := pkg.BackgroundContext()

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles/{z}/{x}/{y}{env.TILEGROXY_TEST_SUFFIX}", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/tiles/1/1/0?key=abc&fmt=png", result)
}

// {ctx.*} resolves from HTTP headers, so like {layer.*} it must be escaped.
func Test_ReplaceURLPlaceholders_CtxValueIsEscaped(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "evil?extra=param")

	ctx := pkg.NewRequestContext(req)

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles/{ctx.User-Agent}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "?extra=param")
	require.Contains(t, result, "evil%3Fextra=param")
}

// An ordinary {ctx.*} value (no special characters) should still round-trip correctly.
func Test_ReplaceURLPlaceholders_CtxValueNormalStillWorks(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "my-agent")

	ctx := pkg.NewRequestContext(req)

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/tiles/{z}/{x}/{y}.png?agent={ctx.User-Agent}", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/tiles/1/1/0.png?agent=my-agent", result)
}

// A template combining a trusted {env.*} value with an untrusted {layer.*} value should escape
// only the latter.
func Test_ReplaceURLPlaceholders_MixedEnvAndLayerOnlyEscapesLayer(t *testing.T) {
	t.Setenv("TILEGROXY_TEST_HOST", "https://tiles.example.com")

	ctx := pkg.BackgroundContext()
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "evil?x=1"

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "{env.TILEGROXY_TEST_HOST}/tiles/{layer.v}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotEmpty(t, result)
	require.Contains(t, result, "https://tiles.example.com/tiles/")
	require.Contains(t, result, "evil%3Fx=1")
	require.NotContains(t, result, "?x=1/")
}

// A request-derived value containing a literal "$N" must never be re-substituted: a repeated-pass
// implementation would replace this "$0" with the {env.*} value, splicing an operator secret into
// the outbound URL. PathEscape leaves "$" alone, so the value stays a literal "$0" here; what
// matters is that it's inert.
func Test_ReplaceURLPlaceholders_CtxValueCannotReinjectPlaceholder(t *testing.T) {
	t.Setenv("TILEGROXY_TEST_SECRET", "super-secret-value")

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "$0")

	ctx := pkg.NewRequestContext(req)

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/{env.TILEGROXY_TEST_SECRET}/{ctx.User-Agent}/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "super-secret-value/super-secret-value")
	require.Equal(t, "https://example.com/super-secret-value/$0/1/1/0.png", result)
}

// The same attack in query position, where url.QueryEscape does percent-encode "$".
func Test_ReplaceURLPlaceholders_CtxValueCannotReinjectPlaceholderInQuery(t *testing.T) {
	t.Setenv("TILEGROXY_TEST_SECRET", "super-secret-value")

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "$0")

	ctx := pkg.NewRequestContext(req)

	result, err := replaceURLPlaceholders(ctx, pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/{env.TILEGROXY_TEST_SECRET}/{z}/{x}/{y}.png?agent={ctx.User-Agent}", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "agent=super-secret-value")
	require.Contains(t, result, "agent=%240")
}

// Templates with more than ten placeholders must still resolve correctly - "$10" has to be read as
// index 10 rather than index 1 followed by a literal "0".
func Test_ReplaceURLPlaceholders_DoubleDigitIndices(t *testing.T) {
	var template string
	var templateSb200 strings.Builder
	for i := range 12 {
		name := "TILEGROXY_TEST_MULTI_" + strconv.Itoa(i)
		t.Setenv(name, "v"+strconv.Itoa(i))
		templateSb200.WriteString("/{env." + name + "}")
	}
	template += templateSb200.String()

	result, err := replaceURLPlaceholders(pkg.BackgroundContext(), pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com"+template, false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.NotContains(t, result, "$")
	for i := range 12 {
		require.Contains(t, result, "/v"+strconv.Itoa(i))
	}
}

// A literal "$" in the template that isn't one of our generated placeholders must pass through
// untouched.
func Test_ReplaceURLPlaceholders_LiteralDollarPreserved(t *testing.T) {
	result, err := replaceURLPlaceholders(pkg.BackgroundContext(), pkg.TileRequest{Z: 1, X: 1, Y: 0}, "https://example.com/a$b/c$/{z}/{x}/{y}.png", false, pkg.SRIDWGS84)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/a$b/c$/1/1/0.png", result)
}
