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
	"fmt"
	"log/slog"
	"slices"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type FallbackConfig struct {
	Primary   map[string]interface{}
	Secondary map[string]interface{}
	Zoom      string
	Bounds    internal.Bounds //Allows any tile that intersects these bounds
}

type Fallback struct {
	zoom      []int
	bounds    internal.Bounds
	Primary   *Provider
	Secondary *Provider
}

func ConstructFallback(config FallbackConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, primary *Provider, secondary *Provider) (*Fallback, error) {
	var zoom []int

	if config.Zoom != "" {
		var err error
		zoom, err = internal.ParseZoomString(config.Zoom)

		if err != nil {
			return nil, err
		}
	} else {
		for z := 0; z <= internal.MaxZoom; z++ {
			zoom = append(zoom, z)
		}
	}

	return &Fallback{zoom, config.Bounds, primary, secondary}, nil
}

func (t Fallback) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return (*t.Primary).PreAuth(ctx, providerContext)
}

func (t Fallback) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	ok := true

	if !slices.Contains(t.zoom, tileRequest.Z) {
		slog.DebugContext(ctx, "Fallback provider falling back due to zoom")
		ok = false
	}

	intersects, err := tileRequest.IntersectsBounds(t.bounds)

	if !intersects || err != nil {
		b, _ := tileRequest.GetBounds()
		slog.DebugContext(ctx, fmt.Sprintf("Fallback provider falling back due to bounds - request %v (%v) vs limit %v", tileRequest, b, t.bounds))
		ok = false
	}

	var img *internal.Image

	if ok {
		img, err = (*t.Primary).GenerateTile(ctx, providerContext, tileRequest)

		if err != nil {
			ok = false
			slog.DebugContext(ctx, fmt.Sprintf("Fallback provider falling back due to error: %v", err.Error()))
		}
	}

	if !ok {
		return (*t.Secondary).GenerateTile(ctx, providerContext, tileRequest)
	}

	return img, err
}
