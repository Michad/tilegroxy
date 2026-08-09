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
// the value into something else (e.g. a URL) can decide whether it needs escaping.
type placeholderSource int

const (
	// sourceEnv is operator-controlled (process environment) - trusted, must not be escaped
	// since it's the documented way to inject things like an entire scheme+host+path prefix.
	sourceEnv placeholderSource = iota
	// sourceCtx is request-derived (HTTP headers/context values) - untrusted.
	sourceCtx
	// sourceLayer is request-derived (pattern matches against the incoming tile request path) - untrusted.
	sourceLayer
	// sourceBuiltin covers the plain string substitutions performed directly by
	// replacePlaceholdersInString (z/x/y/bbox) which never flow through the $N/replacements path.
	sourceBuiltin
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

	for i := range replacements {
		// Make sure longer keys are processed first to avoid e.g. $1's replacement messing up $10
		realI := len(replacements) - i - 1
		placeholder := "$" + strconv.Itoa(realI)
		value := fmt.Sprint(replacements[realI])
		source := sourceFor(realI)

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
		var escaped string
		if source == sourceEnv {
			escaped = value
		} else {
			// Percent-encode the value for the position it lands in: query-escape after the
			// first "?", path-escape before it. url.PathEscape leaves "&", "=", "+", ":", "@"
			// unescaped, which is fine for a path segment on its own, but if this template also
			// has a literal query string later on, a path-position value containing "&" or "="
			// couldn't reorder/inject query params anyway since it's still before the "?". The
			// gap would only matter if the value could smuggle in an unescaped "?" itself, and
			// PathEscape does escape "?", so there's no gap here.
			queryStart := strings.Index(rawURL, "?")
			placeholderIdx := strings.Index(rawURL, placeholder)

			if queryStart >= 0 && placeholderIdx > queryStart {
				escaped = url.QueryEscape(value)
			} else {
				escaped = url.PathEscape(value)
			}
		}

		rawURL = strings.ReplaceAll(rawURL, placeholder, escaped)
	}

	return rawURL, nil
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
