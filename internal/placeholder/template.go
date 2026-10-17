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
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
)

// A config string filled in per request. The consuming Sink decides how each value is escaped
type Template struct {
	raw  string
	segs []segment
}

// Per-request inputs that don't come from the context
type TileVars struct {
	Z, X, Y int
	Bounds  pkg.Bounds
	// Only used to explain unresolved placeholders in logs
	LayerName string
}

func ParseTemplate(str string, r *Resolver) (Template, error) {
	return Template{raw: str}.resolveStatic(r)
}

// Lets the config decoder build a Template. Static placeholders are resolved afterward by a Resolver
func (t *Template) UnmarshalText(text []byte) error {
	t.raw = string(text)
	t.segs = nil

	return nil
}

func (t Template) String() string {
	return t.raw
}

func (t Template) IsEmpty() bool {
	return t.raw == ""
}

// A whole value env.NAME or secret.NAME supplies the template text itself, placeholders included
func (t Template) resolveStatic(r *Resolver) (Template, error) {
	text := t.raw

	if after, found := strings.CutPrefix(text, SourceEnv+"."); found {
		v, err := r.lookup(Ref{Source: SourceEnv, Name: after})
		if err != nil {
			return t, err
		}
		text = v
	} else if after, found := strings.CutPrefix(text, SourceSecret+"."); found {
		v, err := r.lookup(Ref{Source: SourceSecret, Name: after})
		if err != nil {
			return t, err
		}
		text = v
	}

	segs := parse(text)

	for i, seg := range segs {
		if seg.ref == nil || !isStatic(seg.ref.Source) {
			continue
		}

		v, err := r.lookup(*seg.ref)
		if err != nil {
			return t, err
		}

		segs[i].value = v
		segs[i].resolved = true
	}

	return Template{raw: t.raw, segs: segs}, nil
}

func (t Template) segments() []segment {
	if t.segs == nil && t.raw != "" {
		return parse(t.raw)
	}

	return t.segs
}

// Rewrites literal text only, so substituted values are never touched. The rewritten text may add placeholders
func (t Template) MapLiterals(f func(string) string) Template {
	var segs []segment

	for _, seg := range t.segments() {
		if seg.ref != nil {
			segs = append(segs, seg)
			continue
		}

		segs = append(segs, parse(f(seg.literal))...)
	}

	return Template{raw: t.raw, segs: segs}
}

// Reports whether any placeholder reads the given source
func (t Template) Uses(source string) bool {
	for _, seg := range t.segments() {
		if seg.ref != nil && seg.ref.Source == source {
			return true
		}
	}

	return false
}

func (t Template) Render(ctx context.Context, vars TileVars, sink Sink) {
	for _, seg := range t.segments() {
		if seg.ref == nil {
			sink.Literal(seg.literal)
			continue
		}

		sink.Value(resolveValue(ctx, vars, seg))
	}
}

func resolveValue(ctx context.Context, vars TileVars, seg segment) Value {
	ref := *seg.ref

	var text string
	var ok bool

	switch ref.Source {
	case SourceEnv, SourceSecret:
		text, ok = seg.value, seg.resolved
	case SourceTile:
		text, ok = tileValue(vars, ref.Name), true
	case SourceCtx:
		text, ok = stringify(ctx.Value(ref.Name))
	case SourceHeader:
		text, ok = headerValue(ctx, ref.Name)
	case SourceQuery:
		text, ok = queryValue(ctx, ref.Name)
	case SourceLayer:
		text, ok = layerValue(ctx, ref.Name)
	}

	if ok {
		return Value{Text: text, Source: ref.Source}
	}

	if ref.HasDefault {
		return Value{Text: ref.Default, Source: ref.Source}
	}

	slog.Log(ctx, slog.LevelWarn, fmt.Sprintf("%v variable %v could not be resolved while serving layer %v", ref.Source, ref.Name, vars.LayerName))

	return Value{Source: ref.Source, Missing: true}
}

func tileValue(vars TileVars, name string) string {
	switch name {
	case "z", "Z":
		return strconv.Itoa(vars.Z)
	case "x", "X":
		return strconv.Itoa(vars.X)
	case "y", "Y":
		return strconv.Itoa(vars.Y)
	case "xmin":
		return fmt.Sprintf("%f", vars.Bounds.West)
	case "xmax":
		return fmt.Sprintf("%f", vars.Bounds.East)
	case "ymin":
		return fmt.Sprintf("%f", vars.Bounds.South)
	default:
		return fmt.Sprintf("%f", vars.Bounds.North)
	}
}

// Request fields are stored as plain values, identity as pointers, and repeated headers as slices
func stringify(v any) (string, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		v = rv.Elem().Interface()
	}

	switch val := v.(type) {
	case nil:
		return "", false
	case string:
		return val, true
	case []string:
		return strings.Join(val, ","), true
	default:
		return fmt.Sprint(val), true
	}
}

func headerValue(ctx context.Context, name string) (string, bool) {
	req, ok := pkg.ReqFromContext(ctx)
	if !ok || req == nil {
		return "", false
	}

	values := req.Header.Values(name)
	if len(values) == 0 {
		return "", false
	}

	return strings.Join(values, ","), true
}

func queryValue(ctx context.Context, name string) (string, bool) {
	req, ok := pkg.ReqFromContext(ctx)
	if !ok || req == nil || req.URL == nil {
		return "", false
	}

	values, ok := req.URL.Query()[name]
	if !ok {
		return "", false
	}

	return strings.Join(values, ","), true
}

func layerValue(ctx context.Context, name string) (string, bool) {
	matches, _ := pkg.LayerPatternMatchesFromContext(ctx)
	if matches == nil {
		return "", false
	}

	v, ok := (*matches)[name]

	return v, ok
}
