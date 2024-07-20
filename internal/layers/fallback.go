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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type CacheMode string

const (
	CacheModeAlways         = "always"          // Always cache as normal regardless of fallback status
	CacheModeUnlessError    = "unless-error"    // Cache unless the primary provider returns an error
	CacheModeUnlessFallback = "unless-fallback" // Never cache if a fallback occurs (whether due to error, bounds, or zoom)
)

var allCacheModes = []CacheMode{CacheModeAlways, CacheModeUnlessError, CacheModeUnlessFallback}

type FallbackConfig struct {
	Primary   map[string]interface{}
	Secondary map[string]interface{}
	Zoom      string     //Only use Primary for requests in the given range of zoom levels
	Bounds    pkg.Bounds //Allows any tile that intersects these bounds
	Cache     CacheMode  //When to skip cache-ing (in fallback scenarios)
}

type Fallback struct {
	FallbackConfig
	zoomLevels []int
	Primary    Provider
	Secondary  Provider
}

func ConstructFallback(config FallbackConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, primary Provider, secondary Provider) (*Fallback, error) {
	var zoom []int

	if config.Zoom != "" {
		var err error
		zoom, err = pkg.ParseZoomString(config.Zoom)

		if err != nil {
			return nil, err
		}
	} else {
		for z := 0; z <= pkg.MaxZoom; z++ {
			zoom = append(zoom, z)
		}
	}

	if config.Cache == "" {
		config.Cache = CacheModeUnlessError
	}

	if !slices.Contains(allCacheModes, config.Cache) {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.fallback.cachemode", config.Cache, allCacheModes)
	}

	return &Fallback{config, zoom, primary, secondary}, nil
}

func (t Fallback) PreAuth(ctx *pkg.RequestContext, ProviderContext ProviderContext) (ProviderContext, error) {
	return t.Primary.PreAuth(ctx, ProviderContext)
}

func (t Fallback) GenerateTile(ctx *pkg.RequestContext, ProviderContext ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	ok := true

	if !slices.Contains(t.zoomLevels, tileRequest.Z) {
		slog.DebugContext(ctx, "Fallback provider falling back due to zoom")
		ok = false

		if t.Cache == CacheModeUnlessFallback {
			ctx.SkipCacheSave = true
		}
	}

	intersects, err := tileRequest.IntersectsBounds(t.Bounds)

	if !intersects || err != nil {
		b, _ := tileRequest.GetBounds()
		slog.DebugContext(ctx, fmt.Sprintf("Fallback provider falling back due to bounds - request %v (%v) vs limit %v", tileRequest, b, t.Bounds))
		ok = false

		if t.Cache == CacheModeUnlessFallback {
			ctx.SkipCacheSave = true
		}
	}

	var img *pkg.Image

	if ok {
		img, err = t.Primary.GenerateTile(ctx, ProviderContext, tileRequest)

		if err != nil {
			ok = false
			if t.Cache != CacheModeAlways {
				ctx.SkipCacheSave = true
			}

			slog.DebugContext(ctx, fmt.Sprintf("Fallback provider falling back due to error: %v", err.Error()))
		}
	}

	if !ok {

		return t.Secondary.GenerateTile(ctx, ProviderContext, tileRequest)
	}

	return img, err
}
