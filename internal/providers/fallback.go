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
	"log/slog"
	"slices"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
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
	Zoom      string     // Only use Primary for requests in the given range of zoom levels
	Bounds    pkg.Bounds // Allows any tile that intersects these bounds
	Cache     CacheMode  // When to skip cache-ing (in fallback scenarios)
}

type Fallback struct {
	FallbackConfig
	zoomLevels []int
	Primary    layer.Provider
	Secondary  layer.Provider
}

func init() {
	layer.RegisterProvider(FallbackRegistration{})
}

type FallbackRegistration struct {
}

func (s FallbackRegistration) InitializeConfig() any {
	return FallbackConfig{}
}

func (s FallbackRegistration) Name() string {
	return "fallback"
}

func (s FallbackRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *layer.LayerGroup, datastores *datastore.DatastoreRegistry) (layer.Provider, error) {
	cfg := cfgAny.(FallbackConfig)
	var zoom []int

	if cfg.Zoom != "" {
		var err error
		zoom, err = pkg.ParseZoomString(cfg.Zoom)

		if err != nil {
			return nil, err
		}
	} else {
		for z := 0; z <= pkg.MaxZoom; z++ {
			zoom = append(zoom, z)
		}
	}

	if cfg.Cache == "" {
		cfg.Cache = CacheModeUnlessError
	}

	if !slices.Contains(allCacheModes, cfg.Cache) {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.fallback.cachemode", cfg.Cache, allCacheModes)
	}

	primary, err := layer.ConstructProvider(cfg.Primary, clientConfig, errorMessages, layerGroup, datastores)
	if err != nil {
		return nil, err
	}
	secondary, err := layer.ConstructProvider(cfg.Secondary, clientConfig, errorMessages, layerGroup, datastores)
	if err != nil {
		return nil, err
	}

	return &Fallback{cfg, zoom, primary, secondary}, nil
}

func (t Fallback) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return t.Primary.PreAuth(ctx, providerContext)
}

func (t Fallback) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	ok := true

	skipCacheSave := false

	if !slices.Contains(t.zoomLevels, tileRequest.Z) {
		slog.DebugContext(ctx, "Fallback provider falling back due to zoom")
		ok = false

		if t.Cache == CacheModeUnlessFallback {
			skipCacheSave = true
		}
	}

	intersects, err := tileRequest.IntersectsBounds(t.Bounds)

	if !intersects || err != nil {
		b, _ := tileRequest.GetBounds()
		slog.DebugContext(ctx, fmt.Sprintf("Fallback provider falling back due to bounds - request %v (%v) vs limit %v", tileRequest, b, t.Bounds))
		ok = false

		if t.Cache == CacheModeUnlessFallback {
			skipCacheSave = true
		}
	}

	var img *pkg.Image

	if ok {
		img, err = t.Primary.GenerateTile(ctx, providerContext, tileRequest)

		if err != nil {
			ok = false
			if t.Cache != CacheModeAlways {
				skipCacheSave = true
			}

			slog.DebugContext(ctx, fmt.Sprintf("Fallback provider falling back due to error: %v", err.Error()))
		}
	}

	if !ok {
		img, err = t.Secondary.GenerateTile(ctx, providerContext, tileRequest)
	}
	if skipCacheSave {
		img.ForceSkipCache = skipCacheSave
	}

	return img, err
}
