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
	"fmt"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type ProxyConfig struct {
	Url     string
	InvertY bool //Used for TMS
}

type Proxy struct {
	ProxyConfig
	clientConfig *config.ClientConfig
	envRegex     *regexp.Regexp
	ctxRegex     *regexp.Regexp
}

func ConstructProxy(config ProxyConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages) (*Proxy, error) {
	if config.Url == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.proxy.url", "")
	}

	envRegex, err := regexp.Compile(`{env\.[^}]*}`)
	if err != nil {
		return nil, err
	}

	ctxRegex, err := regexp.Compile(`{ctx\.[^}]*}`)
	if err != nil {
		return nil, err
	}

	return &Proxy{config, clientConfig, envRegex, ctxRegex}, nil
}

func (t Proxy) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return ProviderContext{AuthBypass: true}, nil
}

func (t Proxy) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	b, err := tileRequest.GetBounds()

	if err != nil {
		return nil, err
	}

	y := tileRequest.Y
	if t.InvertY {
		y = int(math.Exp2(float64(tileRequest.Z))) - y - 1
	}

	url := t.Url

	if strings.Contains(url, "{env.") {
		envMatches := t.envRegex.FindAllString(url, -1)

		for _, envMatch := range envMatches {
			envVar := envMatch[5 : len(envMatch)-1]

			slog.Debug("Replacing env var " + envVar)

			url = strings.Replace(url, envMatch, os.Getenv(envVar), 1)
		}
	}

	if strings.Contains(url, "{ctx.") {
		ctxMatches := t.ctxRegex.FindAllString(url, -1)

		for _, ctxMatch := range ctxMatches {
			ctxVar := ctxMatch[5 : len(ctxMatch)-1]

			slog.Debug("Replacing context var " + ctxVar)

			url = strings.Replace(url, ctxMatch, fmt.Sprint(ctx.Value(ctxVar)), 1)
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

	return getTile(ctx, t.clientConfig, url, make(map[string]string))
}
