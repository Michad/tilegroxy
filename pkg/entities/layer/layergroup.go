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

package layer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type LayerGroup struct {
	layers           []*Layer
	cacheHitCounter  metric.Int64Counter
	cacheMissCounter metric.Int64Counter
}

func ConstructLayerGroup(cfg config.Config, cache cache.Cache, secreter secret.Secreter) (*LayerGroup, error) {
	var err, err1, err2 error
	var layerGroup LayerGroup
	layerObjects := make([]*Layer, len(cfg.Layers))

	for i, l := range cfg.Layers {
		layerObjects[i], err = ConstructLayer(l, cfg.Client, cfg.Error.Messages, &layerGroup, secreter)
		if err != nil {
			return nil, fmt.Errorf("error constructing layer %v: %w", i, err)
		}

		layerObjects[i].Cache = cache
	}

	meter := otel.Meter(packageName)
	layerGroup.cacheHitCounter, err1 = meter.Int64Counter("tilegroxy.cache.total.hit", metric.WithDescription("Number of requests that hit the cache (ignoring skips)"))
	layerGroup.cacheMissCounter, err2 = meter.Int64Counter("tilegroxy.cache.total.miss", metric.WithDescription("Number of requests that missed the cache (ignoring skips)"))

	layerGroup.layers = layerObjects

	return &layerGroup, errors.Join(err1, err2)
}

func (lg LayerGroup) FindLayer(ctx context.Context, layerName string) *Layer {
	for _, l := range lg.layers {
		if l.MatchesName(ctx, layerName) {
			return l
		}
	}

	return nil
}

func (lg LayerGroup) ListLayerIDs() []string {
	r := make([]string, 0, len(lg.layers))
	for _, l := range lg.layers {
		r = append(r, l.ID)
	}
	return r
}

func (lg LayerGroup) RenderTile(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	var img *pkg.Image
	var err error

	l := lg.FindLayer(ctx, tileRequest.LayerName)

	if l == nil {
		return nil, pkg.UnauthorizedError{Message: "Layer " + tileRequest.LayerName + " does not exist"}
	}

	if l.Config.SkipCache {
		return lg.RenderTileNoCache(ctx, tileRequest)
	}

	err = lg.checkPermission(ctx, l, tileRequest)
	if err != nil {
		return nil, err
	}

	img, err = l.Cache.Lookup(ctx, tileRequest)

	if img != nil {
		slog.DebugContext(ctx, "Cache hit")
		lg.cacheHitCounter.Add(ctx, 1)
		return img, err
	}

	lg.cacheMissCounter.Add(ctx, 1)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cache read error %v\n", err))
	}

	img, err = lg.RenderTileNoCache(ctx, tileRequest)

	if err != nil {
		return nil, err
	}

	ctxSkipCacheSave, _ := pkg.SkipCacheSaveFromContext(ctx)

	if !*ctxSkipCacheSave {
		go func() {
			err = l.Cache.Save(ctx, tileRequest, img)

			if err != nil {
				slog.WarnContext(ctx, fmt.Sprintf("Cache save error %v\n", err))
			}
		}();
	}

	return img, nil
}

func (LayerGroup) checkPermission(ctx context.Context, l *Layer, tileRequest pkg.TileRequest) error {
	ctxLimitLayers, _ := pkg.LimitLayersFromContext(ctx)
	ctxAllowedLayers, _ := pkg.AllowedLayersFromContext(ctx)
	ctxAllowedArea, _ := pkg.AllowedAreaFromContext(ctx)

	if *ctxLimitLayers {
		if !slices.Contains(*ctxAllowedLayers, l.ID) {
			return pkg.UnauthorizedError{Message: "Denying access to non-allowed layer"}
		}
	}

	if !ctxAllowedArea.IsNullIsland() {
		bounds, err := tileRequest.GetBounds()
		if err != nil || !ctxAllowedArea.Contains(*bounds) {
			return pkg.UnauthorizedError{Message: "Denying access to non-allowed area"}
		}
	}
	return nil
}

func (lg *LayerGroup) RenderTileNoCache(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	var err error

	l := lg.FindLayer(ctx, tileRequest.LayerName)

	if l == nil {
		return nil, pkg.UnauthorizedError{Message: "Layer " + tileRequest.LayerName + " does not exist"}
	}

	err = lg.checkPermission(ctx, l, tileRequest)
	if err != nil {
		return nil, err
	}

	return l.RenderTileNoCache(ctx, tileRequest)
}
