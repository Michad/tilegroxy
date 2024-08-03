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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type RefConfig struct {
	Layer string
	// Pattern string
	// Replace map[string][]string
}

type Ref struct {
	RefConfig
	layerGroup *layer.LayerGroup
}

func init() {
	layer.RegisterProvider(RefRegistration{})
}

type RefRegistration struct {
}

func (s RefRegistration) InitializeConfig() any {
	return RefConfig{}
}

func (s RefRegistration) Name() string {
	return "ref"
}

func (s RefRegistration) Initialize(cfgAny any, _ config.ClientConfig, _ config.ErrorMessages, layerGroup *layer.LayerGroup) (layer.Provider, error) {
	cfg := cfgAny.(RefConfig)
	return &Ref{cfg, layerGroup}, nil
}

func (t Ref) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t Ref) GenerateTile(ctx context.Context, _ layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	newRequest := pkg.TileRequest{LayerName: t.Layer, Z: tileRequest.Z, X: tileRequest.X, Y: tileRequest.Y}

	//We need to make a new context for the child call to avoid e.g. layer placeholder from main layer interfering with that of the child layer
	req, _ := pkg.ReqFromContext(ctx)
	newCtx := pkg.NewRequestContext(req)

	//Copy span over from original context
	span := trace.SpanFromContext(ctx)
	newCtx = trace.ContextWithSpan(newCtx, span)

	newCtx, span = makeChildSpan(newCtx, newRequest, "ref", "")
	defer span.End()

	img, err := t.layerGroup.RenderTile(newCtx, newRequest)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from child ref call")
	}

	return img, err
}
