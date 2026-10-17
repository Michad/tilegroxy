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
	"slices"
	"strings"
)

// Where a placeholder's value comes from
const (
	SourceEnv    = "env"
	SourceSecret = "secret"
	SourceCtx    = "ctx"
	SourceHeader = "hdr"
	SourceQuery  = "query"
	SourceLayer  = "layer"
	SourceTile   = "tile"
)

const defaultSeparator = ":-"

var namedSources = []string{SourceEnv, SourceSecret, SourceCtx, SourceHeader, SourceQuery, SourceLayer}

var tileVars = []string{"z", "Z", "x", "X", "y", "Y", "xmin", "xmax", "ymin", "ymax"}

// One placeholder occurrence, e.g. {ctx.tenant:-public}
type Ref struct {
	Source     string
	Name       string
	Default    string
	HasDefault bool
}

func (r Ref) String() string {
	if r.Source == SourceTile {
		return "{" + r.Name + "}"
	}

	if r.HasDefault {
		return "{" + r.Source + "." + r.Name + defaultSeparator + r.Default + "}"
	}

	return "{" + r.Source + "." + r.Name + "}"
}

// Exactly one of literal or ref is meaningful. value is filled in once a static ref is resolved
type segment struct {
	literal  string
	ref      *Ref
	value    string
	resolved bool
}

func isStatic(source string) bool {
	return source == SourceEnv || source == SourceSecret
}

// Anything that doesn't parse as a placeholder stays literal, so braces in SQL or JSON pass through
func parse(str string) []segment {
	var segs []segment
	var lit strings.Builder

	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, segment{literal: lit.String()})
			lit.Reset()
		}
	}

	for i := 0; i < len(str); {
		if str[i] != '{' {
			lit.WriteByte(str[i])
			i++
			continue
		}

		end := strings.IndexAny(str[i+1:], "{}")
		if end < 0 || str[i+1+end] == '{' {
			lit.WriteByte(str[i])
			i++
			continue
		}

		ref, ok := parseRef(str[i+1 : i+1+end])
		if !ok {
			lit.WriteString(str[i : i+2+end])
			i += 2 + end
			continue
		}

		flush()
		segs = append(segs, segment{ref: ref})
		i += 2 + end
	}

	flush()

	return segs
}

func parseRef(inner string) (*Ref, bool) {
	if slices.Contains(tileVars, inner) {
		return &Ref{Source: SourceTile, Name: inner}, true
	}

	source, rest, found := strings.Cut(inner, ".")
	if !found || !slices.Contains(namedSources, source) {
		return nil, false
	}

	name, def, hasDefault := strings.Cut(rest, defaultSeparator)
	if name == "" {
		return nil, false
	}

	return &Ref{Source: source, Name: name, Default: def, HasDefault: hasDefault}, true
}

// Lists every placeholder in str, for checks made against raw configuration
func Refs(str string) []Ref {
	var refs []Ref

	for _, seg := range parse(str) {
		if seg.ref != nil {
			refs = append(refs, *seg.ref)
		}
	}

	return refs
}
