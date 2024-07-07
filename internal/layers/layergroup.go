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
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
)

type LayerGroup struct {
	layers []*Layer
}

func ConstructLayerGroup(cfg config.Config, layers []config.LayerConfig, cache *caches.Cache) (*LayerGroup, error) {
	var err error
	var layerGroup LayerGroup
	layerObjects := make([]*Layer, len(cfg.Layers))

	for i, l := range cfg.Layers {
		layerObjects[i], err = ConstructLayer(l, &cfg.Client, &cfg.Error.Messages, &layerGroup)
		if err != nil {
			return nil, fmt.Errorf("error constructing layer %v: %v", i, err)
		}

		layerObjects[i].Cache = cache
	}

	layerGroup.layers = layerObjects

	return &layerGroup, nil
}

func (lg LayerGroup) FindLayer(ctx *internal.RequestContext, layerName string) *Layer {
	for _, l := range lg.layers {
		if doesMatch, matches := match(l.Pattern, layerName); doesMatch {
			ctx.LayerPatternMatches = matches
			return l
		}
	}

	return nil
}

func (lg LayerGroup) ListLayerIds() []string {
	var r []string
	for _, l := range lg.layers {
		r = append(r, l.Id)
	}
	return r
}

func (lg LayerGroup) RenderTile(ctx *internal.RequestContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	var img *internal.Image
	var err error

	l := lg.FindLayer(ctx, tileRequest.LayerName)

	if l == nil {
		return nil, AuthError{}
	}

	err = lg.checkPermission(ctx, l, tileRequest)
	if err != nil {
		return nil, err
	}

	if l.Config.SkipCache {
		return lg.RenderTileNoCache(ctx, tileRequest)
	}

	img, err = (*l.Cache).Lookup(tileRequest)

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

	err = (*l.Cache).Save(tileRequest, img)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cache save error %v\n", err))
	}

	return img, nil
}

func (LayerGroup) checkPermission(ctx *internal.RequestContext, l *Layer, tileRequest internal.TileRequest) error {
	if ctx.LimitLayers {
		if !slices.Contains(ctx.AllowedLayers, l.Id) {
			slog.InfoContext(ctx, "Denying access to non-allowed layer")
			return AuthError{}
		}
	}

	if !ctx.AllowedArea.IsNullIsland() {
		bounds, err := tileRequest.GetBounds()
		if err != nil || !ctx.AllowedArea.Contains(*bounds) {
			slog.InfoContext(ctx, "Denying access to non-allowed area")
			return AuthError{}
		}
	}
	return nil
}

func (lg *LayerGroup) RenderTileNoCache(ctx *internal.RequestContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	var err error

	l := lg.FindLayer(ctx, tileRequest.LayerName)

	if l == nil {
		return nil, AuthError{}
	}

	err = lg.checkPermission(ctx, l, tileRequest)
	if err != nil {
		return nil, err
	}

	return l.RenderTileNoCache(ctx, tileRequest)
}
