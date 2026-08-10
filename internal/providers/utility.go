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

// placeholderSource identifies where a replacement value originated so callers that splice the
// value into something else (e.g. a URL) can decide whether it needs escaping.
type placeholderSource int

const (
	// Operator-controlled, so trusted.
	sourceEnv placeholderSource = iota
	// Request-derived (HTTP headers), so User input.
	sourceCtx
	// Request-derived (pattern matches against the request path), so User input.
	sourceLayer
)

func replaceURLPlaceholders(ctx context.Context, tileRequest pkg.TileRequest, rawURL string, invertY bool, srid uint) (string, error) {
	// replacePlaceholdersInString assigns $N in a fixed source order, so counting each category up
	// front is enough to classify each $N afterwards. It keeps returning plain values because
	// postgis uses it for SQL params, where the source tag is meaningless.
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

	// A single left-to-right scan, so substituted text is never re-examined. Repeated ReplaceAll
	// passes would be unsafe at any ordering: a request-derived value containing a literal "$0"
	// survives escaping ("$" is escaped by neither PathEscape nor QueryEscape) and a later pass
	// would then splice the operator's unescaped {env.*} value, typically a secret, into the
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

		// {env.*} must be allowed to carry a scheme, host, and slashes, since injecting a whole
		// base URL is the documented use for it. The request-derived sources are escaped: an
		// unescaped "?", "#", or "/" would let a User inject query parameters, truncate the path,
		// or traverse it.
		if sourceFor(idx) == sourceEnv {
			out.WriteString(value)
		} else {
			// Escape for the position the value lands in. Positions are measured against the
			// template, since an {env.*} value substituted in this same scan could contain a "?"
			// and must not retroactively change how later values are escaped.
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
// order starting at startParamIndex. replaceURLPlaceholders depends on that fixed ordering to tell
// which source a given $N came from.
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

// getTile is an alias for the call sites in this package. The implementation lives in pkg so
// library consumers writing their own Go providers can call it too.
func getTile(ctx context.Context, clientConfig config.ClientConfig, url string, authHeaders map[string]string) (*pkg.Image, error) {
	return pkg.GetTile(ctx, clientConfig, url, authHeaders)
}
