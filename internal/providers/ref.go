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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"go.opentelemetry.io/otel/trace"
)

// Maximum number of hops a request may be forwarded through ref providers before we assume a cycle
// and bail out. Startup validation catches statically-resolvable cycles; this is the backstop for
// patterned layer names, which can't be resolved until request time. The root request is hop 0, so
// exactly maxRefDepth ref hops are allowed.
const maxRefDepth = 10

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

func (s RefRegistration) DataType(_ any) config.DataType {
	return config.DataTypeUnknown
}

func (s RefRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(RefConfig)
	return &Ref{cfg, deps.LayerGroup}, nil
}

func (t Ref) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t Ref) GenerateTile(ctx context.Context, _ layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	newRequest := pkg.TileRequest{LayerName: t.Layer, Z: tileRequest.Z, X: tileRequest.X, Y: tileRequest.Y}

	depth, _ := pkg.RefDepthFromContext(ctx)
	if depth != nil && *depth >= maxRefDepth {
		return nil, fmt.Errorf("ref: maximum reference depth (%v) exceeded, likely a cycle involving layer %v", maxRefDepth, t.Layer)
	}

	// We need to make a new context for the child call to avoid e.g. layer placeholder from main layer interfering with that of the child layer
	req, _ := pkg.ReqFromContext(ctx)
	newCtx := pkg.NewRequestContext(req)

	if newDepth, ok := pkg.RefDepthFromContext(newCtx); ok && newDepth != nil && depth != nil {
		*newDepth = *depth + 1
	}

	// Copy span over from original context
	span := trace.SpanFromContext(ctx)
	newCtx = trace.ContextWithSpan(newCtx, span)

	return t.layerGroup.RenderTile(newCtx, newRequest)
}
