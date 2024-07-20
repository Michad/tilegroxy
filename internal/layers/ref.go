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
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
)

type RefConfig struct {
	Layer string
	// Pattern string
	// Replace map[string][]string
}

type Ref struct {
	RefConfig
	layerGroup *LayerGroup
}

func ConstructRef(config RefConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *LayerGroup) (*Ref, error) {
	return &Ref{config, layerGroup}, nil
}

func (t Ref) PreAuth(ctx *pkg.RequestContext, providerContext entities.ProviderContext) (entities.ProviderContext, error) {
	return entities.ProviderContext{AuthBypass: true}, nil
}

func (t Ref) GenerateTile(ctx *pkg.RequestContext, providerContext entities.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	newRequest := pkg.TileRequest{LayerName: t.Layer, Z: tileRequest.Z, X: tileRequest.X, Y: tileRequest.Y}
	newCtx := *ctx
	return t.layerGroup.RenderTile(&newCtx, newRequest)
}
