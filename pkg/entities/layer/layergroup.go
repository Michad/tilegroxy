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
	"fmt"
	"log/slog"
	"slices"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

type LayerGroup struct {
	layers []*Layer
}

func ConstructLayerGroup(cfg config.Config, cache cache.Cache, secreter secret.Secreter) (*LayerGroup, error) {
	var err error
	var layerGroup LayerGroup
	layerObjects := make([]*Layer, len(cfg.Layers))

	for i, l := range cfg.Layers {
		layerObjects[i], err = ConstructLayer(l, cfg.Client, cfg.Error.Messages, &layerGroup, secreter)
		if err != nil {
			return nil, fmt.Errorf("error constructing layer %v: %w", i, err)
		}

		layerObjects[i].Cache = cache
	}

	layerGroup.layers = layerObjects

	return &layerGroup, nil
}

func (lg LayerGroup) FindLayer(ctx *pkg.RequestContext, layerName string) *Layer {
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

func (lg LayerGroup) RenderTile(ctx *pkg.RequestContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
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

	img, err = l.Cache.Lookup(tileRequest)

	if img != nil {
		slog.DebugContext(ctx, "Cache hit")
		return img, err
	}

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cache read error %v\n", err))
	}

	img, err = lg.RenderTileNoCache(ctx, tileRequest)

	if err != nil {
		return nil, err
	}

	if !ctx.SkipCacheSave {
		err = l.Cache.Save(tileRequest, img)

		if err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("Cache save error %v\n", err))
		}
	}

	return img, nil
}

func (LayerGroup) checkPermission(ctx *pkg.RequestContext, l *Layer, tileRequest pkg.TileRequest) error {
	if ctx.LimitLayers {
		if !slices.Contains(ctx.AllowedLayers, l.ID) {
			return pkg.UnauthorizedError{Message: "Denying access to non-allowed layer"}
		}
	}

	if !ctx.AllowedArea.IsNullIsland() {
		bounds, err := tileRequest.GetBounds()
		if err != nil || !ctx.AllowedArea.Contains(*bounds) {
			return pkg.UnauthorizedError{Message: "Denying access to non-allowed area"}
		}
	}
	return nil
}

func (lg *LayerGroup) RenderTileNoCache(ctx *pkg.RequestContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
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
