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
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

const mimePng = "image/png"

var envRegex = regexp.MustCompile(`{env\.[^{}}]*}`)
var ctxRegex = regexp.MustCompile(`{ctx\.[^{}}]*}`)
var lyrRegex = regexp.MustCompile(`{layer\.[^{}}]*}`)

const mvtContentType = "application/vnd.mapbox-vector-tile"
const mltContentType = images.MltContentType

// Lets callers splicing a value into something else, like a URL, decide whether it needs escaping
type placeholderSource int

const (
	// Operator-controlled, so trusted
	sourceEnv placeholderSource = iota
	// From HTTP headers, so User input
	sourceCtx
	// From pattern matches against the request path, so User input
	sourceLayer
)

func replaceURLPlaceholders(ctx context.Context, tileRequest pkg.TileRequest, rawURL string, invertY bool, srid uint) (string, error) {
	// $N is assigned in a fixed source order, so counts are enough to classify each. postgis needs only plain values
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

	// One scan so substituted text is never re-examined. Repeated passes could splice a "$0" from User input into a secret
	var out strings.Builder
	queryStart := strings.Index(rawURL, "?")

	for i := 0; i < len(rawURL); {
		if rawURL[i] != '$' {
			out.WriteByte(rawURL[i])
			i++
			continue
		}

		// Longest digit run so "$10" is index 10, not index 1 then "0"
		j := i + 1
		for j < len(rawURL) && rawURL[j] >= '0' && rawURL[j] <= '9' {
			j++
		}

		idx, err := strconv.Atoi(rawURL[i+1 : j])
		if j == i+1 || err != nil || idx >= len(replacements) {
			// A literal "$" or an out of range index passes through untouched
			out.WriteByte(rawURL[i])
			i++
			continue
		}

		value := fmt.Sprint(replacements[idx])

		// {env.*} may inject a whole base URL. Request-derived values are escaped so "?", "#" or "/" can't alter the URL
		if sourceFor(idx) == sourceEnv {
			out.WriteString(value)
		} else {
			// Measured against the template so a "?" inside an {env.*} value can't change how later values are escaped
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

// E.g. "a {env.foo}" -> "a $1" with {"$1": "bar"}. Coordinates are inlined. $N follows env, ctx, then layer order
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
			envVarVal, envVarExists := os.LookupEnv(envVar)

			if !envVarExists {
				slog.Log(ctx, slog.LevelWarn, fmt.Sprintf("env variable %v could not be resolved while serving layer %v", envVar, tileRequest.LayerName))
			}

			replacements = append(replacements, envVarVal)
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

			if valVal.Kind() == reflect.Pointer {
				val = valVal.Elem().Interface()
			}

			if val == nil {
				slog.Log(ctx, slog.LevelWarn, fmt.Sprintf("ctx variable %v could not be resolved while serving layer %v", ctxVar, tileRequest.LayerName))
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
			valExists := false

			if lpm != nil {
				val, valExists = (*lpm)[layerVar]
			}

			if !valExists {
				slog.Log(ctx, slog.LevelWarn, fmt.Sprintf("layer variable %v could not be resolved while serving layer %v", layerVar, tileRequest.LayerName))
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

// Lives in pkg so library consumers writing Go providers can call it too
func getTile(ctx context.Context, clientConfig config.ClientConfig, url string, authHeaders map[string]string) (*pkg.Image, error) {
	return pkg.GetTile(ctx, clientConfig, url, authHeaders)
}

// Format `<zoom>|<zoom>-<zoom>[,<range>]`, e.g. `4`, `1-5` or `1-3,6`
func ParseZoomString(str string) ([]int, error) {
	const errorMessage = "could not parse zoom %v"

	commaSplit := strings.Split(str, ",")

	var result []int

	for _, entry := range commaSplit {
		dashSplit := strings.Split(entry, "-")

		switch len(dashSplit) {
		case 1:
			singleZoom, err := strconv.Atoi(dashSplit[0])

			if singleZoom < 0 || singleZoom > pkg.MaxZoom {
				return nil, errors.New("zoom out of range")
			}

			if err == nil {
				result = append(result, singleZoom)
			} else {
				return nil, fmt.Errorf(errorMessage, entry)
			}
		case 2:
			start, err := strconv.Atoi(dashSplit[0])
			end, err2 := strconv.Atoi(dashSplit[1])
			if err != nil || err2 != nil {
				return nil, errors.Join(err, err2)
			}

			if end < start {
				return nil, errors.New("zoom range must start before it ends")
			}

			if start < 0 || end > pkg.MaxZoom {
				return nil, errors.New("zoom out of range")
			}

			for i := start; i <= end; i++ {
				result = append(result, i)
			}
		default:
			return nil, fmt.Errorf(errorMessage, entry)
		}
	}

	return result, nil
}

// Rejects a child known to produce a data type the caller can't process
func checkForInvalidDataType(providerToCheck layer.Provider, invalidType config.DataType, path string, errorMessages config.ErrorMessages) error {
	if layer.DescribeTree(providerToCheck).DataType == invalidType {
		return fmt.Errorf(errorMessages.InvalidParam, path, string(invalidType))
	}

	return nil
}
