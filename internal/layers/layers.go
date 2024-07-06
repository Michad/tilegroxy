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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
)

type LayerGroup struct {
	layers []*Layer
}

func ConstructLayerGroup(cfg config.Config, layers []config.LayerConfig, cache *caches.Cache) (*LayerGroup, error) {
	var err error
	layerObjects := make([]*Layer, len(cfg.Layers))

	for i, l := range cfg.Layers {
		layerObjects[i], err = ConstructLayer(l, &cfg.Client, &cfg.Error.Messages)
		if err != nil {
			return nil, fmt.Errorf("error constructing layer %v: %v", i, err)
		}

		layerObjects[i].Cache = cache
	}

	return &LayerGroup{layerObjects}, nil
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
