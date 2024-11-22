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
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const mimePng = "image/png"

var envRegex = regexp.MustCompile(`{env\.[^{}}]*}`)
var ctxRegex = regexp.MustCompile(`{ctx\.[^{}}]*}`)
var lyrRegex = regexp.MustCompile(`{layer\.[^{}}]*}`)

const mvtContentType = "application/vnd.mapbox-vector-tile"

func replaceURLPlaceholders(ctx context.Context, tileRequest pkg.TileRequest, url string, invertY bool, srid uint) (string, error) {
	url, replacements, err := replacePlaceholdersInString(ctx, tileRequest, url, 0, invertY, srid)

	if err != nil {
		return "", err
	}

	for i := range replacements {
		// Make sure longer keys are processed first to avoid e.g. $1's replacement messing up $10
		realI := len(replacements) - i - 1
		url = strings.ReplaceAll(url, "$"+strconv.Itoa(realI), fmt.Sprint(replacements[realI]))
	}

	return url, nil
}

// Replaces arbitrary application specific placeholders in an arbitrary string with more generic prepared statement style placeholders and returns a mapping of those final placeholders to the real values.  e.g. "blah {env.foo} blah" -> "blah $1 blah" and {"$1": "bar"}
// Values that are guaranteed to be safe (such as tile coordinates) are replaced directly in the string
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

/**
 * Performs a GET operation against a given URL. Implementing providers should call this when possible. It has
 * standard reusable logic around various config options
 */
func getTile(ctx context.Context, clientConfig config.ClientConfig, url string, authHeaders map[string]string) (*pkg.Image, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Calling url %v\n", url))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", clientConfig.UserAgent)

	for h, v := range clientConfig.Headers {
		req.Header.Set(h, v)
	}

	for h, v := range authHeaders {
		req.Header.Set(h, v)
	}

	if clientConfig.Timeout > math.MaxInt32 {
		clientConfig.Timeout = math.MaxInt32
	}

	transport := otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithMessageEvents(otelhttp.ReadEvents))
	client := http.Client{Transport: transport, Timeout: time.Duration(clientConfig.Timeout) * time.Second}

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, fmt.Sprintf("Response status: %v", resp.StatusCode))

	if !slices.Contains(clientConfig.StatusCodes, resp.StatusCode) {
		return nil, &pkg.RemoteServerError{StatusCode: resp.StatusCode}
	}

	contentType := resp.Header.Get("Content-Type")

	if !slices.Contains(clientConfig.ContentTypes, contentType) {
		return nil, &pkg.InvalidContentTypeError{ContentType: contentType}
	}

	if clientConfig.RewriteContentTypes != nil {
		newContentType, ok := clientConfig.RewriteContentTypes[contentType]

		if ok {
			contentType = newContentType
		}
	}

	if resp.ContentLength == -1 {
		if !clientConfig.UnknownLength {
			return nil, &pkg.InvalidContentLengthError{Length: -1}
		}
	} else {
		if resp.ContentLength > int64(clientConfig.MaxLength) {
			return nil, &pkg.InvalidContentLengthError{Length: int(resp.ContentLength)}
		}
	}

	img, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, &pkg.RemoteServerError{StatusCode: resp.StatusCode}
	}

	if len(img) > clientConfig.MaxLength {
		return nil, &pkg.InvalidContentLengthError{Length: len(img)}
	}

	return &pkg.Image{Content: img, ContentType: contentType}, nil
}
