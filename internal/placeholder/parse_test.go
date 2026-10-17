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
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Refs_AllForms(t *testing.T) {
	refs := Refs("{z}/{env.A}/{secret.b}/{ctx.user}/{hdr.X-Id}/{query.q}/{layer.v:-def}/{ctx.e:-}")

	assert.Equal(t, []Ref{
		{Source: SourceTile, Name: "z"},
		{Source: SourceEnv, Name: "A"},
		{Source: SourceSecret, Name: "b"},
		{Source: SourceCtx, Name: "user"},
		{Source: SourceHeader, Name: "X-Id"},
		{Source: SourceQuery, Name: "q"},
		{Source: SourceLayer, Name: "v", Default: "def", HasDefault: true},
		{Source: SourceCtx, Name: "e", Default: "", HasDefault: true},
	}, refs)
}

// SQL and JSON braces must pass through untouched
func Test_Parse_NonPlaceholdersStayLiteral(t *testing.T) {
	for _, str := range []string{
		`props @> '{"a":1}'`,
		"{unknown.thing}",
		"{env.}",
		"{zoom}",
		"{env.A",
		"no braces",
		"}{",
		"",
	} {
		t.Run(str, func(t *testing.T) {
			assert.Empty(t, Refs(str))

			var sink RawSink
			for _, seg := range parse(str) {
				sink.Literal(seg.literal)
			}
			assert.Equal(t, str, sink.String())
		})
	}
}

func Test_Parse_NestedOpenBraceKeepsInnerPlaceholder(t *testing.T) {
	segs := parse("{{z}}")

	assert.Len(t, segs, 3)
	assert.Equal(t, "{", segs[0].literal)
	assert.Equal(t, &Ref{Source: SourceTile, Name: "z"}, segs[1].ref)
	assert.Equal(t, "}", segs[2].literal)
}

func Test_Ref_String(t *testing.T) {
	for _, str := range []string{"{z}", "{env.A}", "{ctx.tenant:-public}", "{layer.v:-}"} {
		assert.Equal(t, str, Refs(str)[0].String())
	}
}
