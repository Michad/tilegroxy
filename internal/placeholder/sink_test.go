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
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderURL(ctx context.Context, t *testing.T, str string) string {
	t.Helper()

	tmpl, err := ParseTemplate(str, secretResolver())
	require.NoError(t, err)

	var sink URLSink
	tmpl.Render(ctx, testVars, &sink)

	return sink.String()
}

func Test_URLSink_EscapesUntrustedByPosition(t *testing.T) {
	ctx := requestContext(t, "http://a/", nil)
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "../a?b#c d"

	assert.Equal(t, "https://e.com/..%2Fa%3Fb%23c%20d/3?v=..%2Fa%3Fb%23c+d", renderURL(ctx, t, "https://e.com/{layer.v}/{z}?v={layer.v}"))
}

func Test_URLSink_TrustedValuesInsertedAsIs(t *testing.T) {
	t.Setenv("TEST_SINK_BASE", "https://e.com/base?key=1")
	ctx := requestContext(t, "http://a/", nil)
	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*lpm)["v"] = "a/b"

	// The "?" inside the env value doesn't switch later values to query escaping
	assert.Equal(t, "https://e.com/base?key=1/a%2Fb&p=hunter2", renderURL(ctx, t, "{env.TEST_SINK_BASE}/{layer.v}&p={secret.pw}"))
}

func Test_URLSink_DefaultEscapedLikeValue(t *testing.T) {
	assert.Equal(t, "/a%2Fb?x=a%2Fb", renderURL(context.Background(), t, "/{layer.v:-a/b}?x={layer.v:-a/b}"))
}

func Test_SQLSink_ParametersEverythingButTiles(t *testing.T) {
	t.Setenv("TEST_SINK_COL", "O'Brien")

	tmpl, err := ParseTemplate("{z} > 0 AND name = {env.TEST_SINK_COL} AND v = {layer.v} AND m = {layer.missing} AND d = {hdr.x:-dflt}", nil)
	require.NoError(t, err)

	sink := NewSQLSink(8)
	tmpl.Render(requestContext(t, "http://a/", nil), testVars, sink)

	assert.Equal(t, "3 > 0 AND name = $8 AND v = $9 AND m = $10 AND d = $11", sink.String())
	assert.Equal(t, []any{"O'Brien", "matched", nil, "dflt"}, sink.Params())
}

func Test_Value_Trusted(t *testing.T) {
	for _, source := range []string{SourceEnv, SourceSecret, SourceTile} {
		assert.True(t, Value{Source: source}.Trusted(), source)
	}

	for _, source := range []string{SourceCtx, SourceHeader, SourceQuery, SourceLayer} {
		assert.False(t, Value{Source: source}.Trusted(), source)
	}
}
