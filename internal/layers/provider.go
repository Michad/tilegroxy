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

package layers

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/mitchellh/mapstructure"
)

type Provider interface {
	// Performs authentication before tiles are ever generated. The calling code ensures this is only called once at a time and only when needed
	// based on the expiration in ProviderContext and when an AuthError is returned from GenerateTile
	PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error)
	GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error)
}

func ConstructProvider(rawConfig map[string]interface{}, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *LayerGroup) (Provider, error) {
	rawConfig = internal.ReplaceEnv(rawConfig)

	if rawConfig["name"] == "url template" {
		var config UrlTemplateConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}

		return ConstructUrlTemplate(config, clientConfig, errorMessages)
	} else if rawConfig["name"] == "proxy" {
		var config ProxyConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructProxy(config, clientConfig, errorMessages)
	} else if rawConfig["name"] == "custom" {
		var config CustomConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructCustom(config, clientConfig, errorMessages)
	} else if rawConfig["name"] == "static" {
		var config StaticConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructStatic(config, clientConfig, errorMessages)
	} else if rawConfig["name"] == "ref" {
		var config RefConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructRef(config, clientConfig, errorMessages, layerGroup)
	} else if rawConfig["name"] == "fallback" {
		var config FallbackConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		primary, err := ConstructProvider(config.Primary, clientConfig, errorMessages, layerGroup)
		if err != nil {
			return nil, err
		}
		secondary, err := ConstructProvider(config.Secondary, clientConfig, errorMessages, layerGroup)
		if err != nil {
			return nil, err
		}

		return ConstructFallback(config, clientConfig, errorMessages, primary, secondary)
	} else if rawConfig["name"] == "blend" {
		var config BlendConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		var providers []Provider
		var errorSlice []error
		for _, p := range config.Providers {
			provider, err := ConstructProvider(p, clientConfig, errorMessages, layerGroup)
			providers = append(providers, provider)
			errorSlice = append(errorSlice, err)
		}

		errorsFlat := errors.Join(errorSlice...)
		if errorsFlat != nil {
			return nil, errorsFlat
		}

		return ConstructBlend(config, clientConfig, errorMessages, providers, layerGroup)
	} else if rawConfig["name"] == "effect" {
		var config EffectConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		child, err := ConstructProvider(config.Provider, clientConfig, errorMessages, layerGroup)
		if err != nil {
			return nil, err
		}

		return ConstructEffect(config, clientConfig, errorMessages, child)
	} else if rawConfig["name"] == "transform" {
		var config TransformConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}

		child, err := ConstructProvider(config.Provider, clientConfig, errorMessages, layerGroup)
		if err != nil {
			return nil, err
		}

		return ConstructTransform(config, clientConfig, errorMessages, child)
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.name", name)
}

type ProviderContext struct {
	AuthBypass     bool                   //If true, avoids ever calling preauth again
	AuthExpiration time.Time              //When next to trigger preauth
	AuthToken      string                 //The main auth token that comes back from the preauth and is used by the generate method. Details are up to the provider
	Other          map[string]interface{} //A generic holder in cases where a provider needs extra storage - for instance Blend which needs Context for child providers
}

type AuthError struct {
	Message string
}

func (e AuthError) Error() string {
	// notest
	return fmt.Sprintf("Auth Error - %s", e.Message)
}

type InvalidContentLengthError struct {
	Length int
}

func (e *InvalidContentLengthError) Error() string {
	// notest
	return fmt.Sprintf("Invalid content length %v", e.Length)
}

type InvalidContentTypeError struct {
	ContentType string
}

func (e *InvalidContentTypeError) Error() string {
	// notest
	return fmt.Sprintf("Invalid content type %v", e.ContentType)
}

type RemoteServerError struct {
	StatusCode int
}

func (e *RemoteServerError) Error() string {
	// notest
	return fmt.Sprintf("Remote server returned status code %v", e.StatusCode)
}

var envRegex, _ = regexp.Compile(`{env\.[^{}}]*}`)
var ctxRegex, _ = regexp.Compile(`{ctx\.[^{}}]*}`)
var lyrRegex, _ = regexp.Compile(`{layer\.[^{}}]*}`)

func replaceUrlPlaceholders(ctx *internal.RequestContext, tileRequest internal.TileRequest, url string, invertY bool) (string, error) {
	b, err := tileRequest.GetBounds()

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

			url = strings.Replace(url, ctxMatch, fmt.Sprint(ctx.Value(ctxVar)), 1)
		}
	}

	if strings.Contains(url, "{layer.") {
		layerMatches := lyrRegex.FindAllString(url, -1)

		for _, layerMatch := range layerMatches {
			layerVar := layerMatch[7 : len(layerMatch)-1]

			slog.Debug("Replacing layer var " + layerVar)

			url = strings.Replace(url, layerMatch, fmt.Sprint(ctx.LayerPatternMatches[layerVar]), 1)
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
func getTile(ctx *internal.RequestContext, clientConfig config.ClientConfig, url string, authHeaders map[string]string) (*internal.Image, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Calling url %v\n", url))

	req, err := http.NewRequest(http.MethodGet, url, nil)
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

	client := http.Client{Timeout: time.Duration(clientConfig.Timeout) * time.Second}

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, fmt.Sprintf("Response status: %v", resp.StatusCode))

	if !slices.Contains(clientConfig.StatusCodes, resp.StatusCode) {
		return nil, &RemoteServerError{StatusCode: resp.StatusCode}
	}

	if !slices.Contains(clientConfig.ContentTypes, resp.Header.Get("Content-Type")) {
		return nil, &InvalidContentTypeError{ContentType: resp.Header.Get("Content-Type")}
	}

	if resp.ContentLength == -1 {
		if !clientConfig.UnknownLength {
			return nil, &InvalidContentLengthError{-1}
		}
	} else {
		if resp.ContentLength > int64(clientConfig.MaxLength) {
			return nil, &InvalidContentLengthError{int(resp.ContentLength)}
		}
	}

	img, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, &RemoteServerError{StatusCode: resp.StatusCode}
	}

	if len(img) > int(clientConfig.MaxLength) {
		return nil, &InvalidContentLengthError{int(len(img))}
	}

	return &img, nil
}
