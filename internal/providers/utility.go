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

func replaceURLPlaceholders(ctx context.Context, tileRequest pkg.TileRequest, url string, invertY bool, srid uint) (string, error) {
	b, err := tileRequest.GetBoundsProjection(srid)

	if err != nil {
		return "", err
	}

	y := tileRequest.Y
	if invertY {
		y = int(math.Exp2(float64(tileRequest.Z))) - y - 1
	}

	if strings.Contains(url, "{env.") {
		envMatches := envRegex.FindAllString(url, -1)

		for _, envMatch := range envMatches {
			envVar := envMatch[5 : len(envMatch)-1]

			slog.Debug("Replacing env var " + envVar)

			url = strings.Replace(url, envMatch, os.Getenv(envVar), 1)
		}
	}

	if strings.Contains(url, "{ctx.") {
		ctxMatches := ctxRegex.FindAllString(url, -1)

		for _, ctxMatch := range ctxMatches {
			ctxVar := ctxMatch[5 : len(ctxMatch)-1]

			slog.Debug("Replacing context var " + ctxVar)

			val := ctx.Value(ctxVar)
			valVal := reflect.ValueOf(val)

			if valVal.Kind() == reflect.Ptr {
				val = valVal.Elem().Interface()
			}

			url = strings.Replace(url, ctxMatch, fmt.Sprint(val), 1)
		}
	}

	if strings.Contains(url, "{layer.") {
		layerMatches := lyrRegex.FindAllString(url, -1)

		for _, layerMatch := range layerMatches {
			layerVar := layerMatch[7 : len(layerMatch)-1]

			slog.Debug("Replacing layer var " + layerVar)

			lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)
			url = strings.Replace(url, layerMatch, (*lpm)[layerVar], 1)
		}
	}

	url = strings.ReplaceAll(url, "{Z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{Y}", strconv.Itoa(y))
	url = strings.ReplaceAll(url, "{y}", strconv.Itoa(y))
	url = strings.ReplaceAll(url, "{X}", strconv.Itoa(tileRequest.X))
	url = strings.ReplaceAll(url, "{x}", strconv.Itoa(tileRequest.X))

	url = strings.ReplaceAll(url, "{xmin}", fmt.Sprintf("%f", b.West))
	url = strings.ReplaceAll(url, "{xmax}", fmt.Sprintf("%f", b.East))
	url = strings.ReplaceAll(url, "{ymin}", fmt.Sprintf("%f", b.South))
	url = strings.ReplaceAll(url, "{ymax}", fmt.Sprintf("%f", b.North))
	return url, nil
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
