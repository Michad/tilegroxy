// Copyright 2024 Michael Davis
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
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

const mimePng = "image/png"

var envRegex = regexp.MustCompile(`{env\.[^{}}]*}`)
var ctxRegex = regexp.MustCompile(`{ctx\.[^{}}]*}`)
var lyrRegex = regexp.MustCompile(`{layer\.[^{}}]*}`)

const mvtContentType = "application/vnd.mapbox-vector-tile"

// placeholderSource identifies where a replacement value originated so callers that splice
// the value into something else (e.g. a URL) can decide whether it needs escaping. Note the
// plain string substitutions performed directly by replacePlaceholdersInString (z/x/y/bbox)
// never flow through the $N/replacements path, so they have no source of their own.
type placeholderSource int

const (
	// sourceEnv is operator-controlled (process environment) - trusted, must not be escaped
	// since it's the documented way to inject things like an entire scheme+host+path prefix.
	sourceEnv placeholderSource = iota
	// sourceCtx is request-derived (HTTP headers/context values) - untrusted.
	sourceCtx
	// sourceLayer is request-derived (pattern matches against the incoming tile request path) - untrusted.
	sourceLayer
)

func replaceURLPlaceholders(ctx context.Context, tileRequest pkg.TileRequest, rawURL string, invertY bool, srid uint) (string, error) {
	// replacePlaceholdersInString processes {env.*} matches first, then {ctx.*}, then {layer.*}
	// (see below), assigning $N in that order starting at 0. Count each category up front so we
	// can classify each $N by source afterward without changing that function's shared signature
	// (it's also used by postgis for SQL params, where raw values - not source-tagged ones - are
	// required).
	envCount := len(envRegex.FindAllString(rawURL, -1))
	ctxCount := len(ctxRegex.FindAllString(rawURL, -1))

	rawURL, replacements, err := replacePlaceholdersInString(ctx, tileRequest, rawURL, 0, invertY, srid)

	if err != nil {
		return "", err
	}

	sourceFor := func(idx int) placeholderSource {
		switch {
		case idx < envCount:
			return sourceEnv
		case idx < envCount+ctxCount:
			return sourceCtx
		default:
			return sourceLayer
		}
	}

	// Substitution is a single left-to-right scan that appends to a separate builder, so text that
	// has already been substituted is never re-examined. Repeated ReplaceAll passes over the whole
	// URL would be unsafe here even processing high indices first: descending order stops $1's
	// replacement from corrupting $10, but a *value* inserted by an earlier pass is still visible
	// to every later pass. A request-derived value containing the literal "$0" survives escaping
	// (url.PathEscape and url.QueryEscape both leave "$" unescaped) and would then be replaced by
	// the $0 pass - splicing an operator's unescaped {env.*} value, typically a secret, into the
	// outbound URL and the debug log. One scan makes inserted text inert by construction.
	var out strings.Builder
	queryStart := strings.Index(rawURL, "?")

	for i := 0; i < len(rawURL); {
		if rawURL[i] != '$' {
			out.WriteByte(rawURL[i])
			i++
			continue
		}

		// Take the longest run of digits after '$' so "$10" reads as index 10, not index 1
		// followed by a literal "0".
		j := i + 1
		for j < len(rawURL) && rawURL[j] >= '0' && rawURL[j] <= '9' {
			j++
		}

		idx, err := strconv.Atoi(rawURL[i+1 : j])
		if j == i+1 || err != nil || idx >= len(replacements) {
			// Not a placeholder this call produced (a literal "$", or an index out of range):
			// emit it untouched.
			out.WriteByte(rawURL[i])
			i++
			continue
		}

		value := fmt.Sprint(replacements[idx])

		// Placeholder values come from three sources with different trust levels:
		//   - {env.*} is operator config (os.Getenv) - it must be allowed to contain a
		//     scheme, host, and slashes (e.g. a base URL), so it is NOT escaped.
		//   - {ctx.*} and {layer.*} are request-derived (HTTP headers / pattern matches
		//     against the incoming request path) - an unescaped "?", "#", or "/" would let
		//     an attacker inject query parameters, a fragment, or rewrite/traverse the path.
		//     These ARE escaped. {ctx.*} is documented for things like auth tokens/header
		//     passthrough, not path segments, so escaping it costs no legitimate use case
		//     we're aware of; if a future use needs literal slashes from {ctx.*}, that's a
		//     deliberate tradeoff to revisit, not an oversight.
		if sourceFor(idx) == sourceEnv {
			out.WriteString(value)
		} else {
			// Percent-encode the value for the position it lands in: query-escape after the
			// first "?", path-escape before it. Positions are measured against the template,
			// since an {env.*} value substituted earlier in this same scan could itself contain
			// a "?" and must not retroactively change how later values are escaped. url.PathEscape
			// leaves "&", "=", "+", ":", "@" unescaped, which is fine for a path segment on its
			// own, but if this template also has a literal query string later on, a path-position
			// value containing "&" or "=" couldn't reorder/inject query params anyway since it's
			// still before the "?". The gap would only matter if the value could smuggle in an
			// unescaped "?" itself, and PathEscape does escape "?", so there's no gap here.
			if queryStart >= 0 && i > queryStart {
				out.WriteString(url.QueryEscape(value))
			} else {
				out.WriteString(url.PathEscape(value))
			}
		}

		i = j
	}

	return out.String(), nil
}

// Replaces arbitrary application specific placeholders in an arbitrary string with more generic prepared statement style placeholders and returns a mapping of those final placeholders to the real values.  e.g. "blah {env.foo} blah" -> "blah $1 blah" and {"$1": "bar"}
// Values that are guaranteed to be safe (such as tile coordinates) are replaced directly in the string.
// {env.*} matches are processed first, then {ctx.*}, then {layer.*}, each assigning $N in that
// order starting at startParamIndex - callers that need to know which source a given $N came from
// (e.g. replaceURLPlaceholders, to decide whether to escape it) rely on that fixed ordering rather
// than a source tag on each element, so this keeps returning plain values for callers like postgis
// that just need them as SQL prepared statement params.
func replacePlaceholdersInString(ctx context.Context, tileRequest pkg.TileRequest, str string, startParamIndex int, invertY bool, srid uint) (string, []any, error) {
	b, err := tileRequest.GetBoundsProjection(srid)

	if err != nil {
		return "", nil, err
	}

	replacements := make([]any, 0)
	paramIndex := startParamIndex

	y := tileRequest.Y
	if invertY {
		y = int(math.Exp2(float64(tileRequest.Z))) - y - 1
	}

	if strings.Contains(str, "{env.") {
		envMatches := envRegex.FindAllString(str, -1)

		for _, envMatch := range envMatches {
			envVar := envMatch[5 : len(envMatch)-1]

			param := "$" + strconv.Itoa(paramIndex)
			replacements = append(replacements, os.Getenv(envVar))
			str = strings.Replace(str, envMatch, param, 1)
			paramIndex++
		}
	}

	if strings.Contains(str, "{ctx.") {
		ctxMatches := ctxRegex.FindAllString(str, -1)

		for _, ctxMatch := range ctxMatches {
			ctxVar := ctxMatch[5 : len(ctxMatch)-1]

			val := ctx.Value(ctxVar)
			valVal := reflect.ValueOf(val)

			if valVal.Kind() == reflect.Ptr {
				val = valVal.Elem().Interface()
			}

			param := "$" + strconv.Itoa(paramIndex)
			replacements = append(replacements, fmt.Sprint(val))
			str = strings.Replace(str, ctxMatch, param, 1)
			paramIndex++
		}
	}

	if strings.Contains(str, "{layer.") {
		layerMatches := lyrRegex.FindAllString(str, -1)

		lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)

		for _, layerMatch := range layerMatches {
			layerVar := layerMatch[7 : len(layerMatch)-1]

			param := "$" + strconv.Itoa(paramIndex)
			var val any

			if lpm != nil {
				val = (*lpm)[layerVar]
			}

			replacements = append(replacements, val)
			str = strings.Replace(str, layerMatch, param, 1)
			paramIndex++
		}
	}

	str = strings.ReplaceAll(str, "{Z}", strconv.Itoa(tileRequest.Z))
	str = strings.ReplaceAll(str, "{z}", strconv.Itoa(tileRequest.Z))
	str = strings.ReplaceAll(str, "{Y}", strconv.Itoa(y))
	str = strings.ReplaceAll(str, "{y}", strconv.Itoa(y))
	str = strings.ReplaceAll(str, "{X}", strconv.Itoa(tileRequest.X))
	str = strings.ReplaceAll(str, "{x}", strconv.Itoa(tileRequest.X))

	str = strings.ReplaceAll(str, "{xmin}", fmt.Sprintf("%f", b.West))
	str = strings.ReplaceAll(str, "{xmax}", fmt.Sprintf("%f", b.East))
	str = strings.ReplaceAll(str, "{ymin}", fmt.Sprintf("%f", b.South))
	str = strings.ReplaceAll(str, "{ymax}", fmt.Sprintf("%f", b.North))
	return str, replacements, nil
}

// getTile is kept as a thin alias so existing call sites in this package don't need to change;
// the real implementation lives in pkg.GetTile so library consumers writing real Go providers
// (not just yaegi scripts, which reach it via the injection in custom.go) can call it too.
func getTile(ctx context.Context, clientConfig config.ClientConfig, url string, authHeaders map[string]string) (*pkg.Image, error) {
	return pkg.GetTile(ctx, clientConfig, url, authHeaders)
}
