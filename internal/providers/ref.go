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
	"strings"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"go.opentelemetry.io/otel/trace"
)

// Backstop for chains of names that never repeat, such as ones that grow each hop. The root request is hop 0
const maxRefDepth = 10

type refChainKey struct{}

type RefConfig struct {
	Layer string
}

type Ref struct {
	RefConfig
	layerGroup layer.LayerGroup
	target     layer.Description
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
	ref := &Ref{RefConfig: cfg, layerGroup: deps.LayerGroup}

	if lg, ok := deps.LayerGroup.(*layers.LayerGroup); ok && lg != nil && !strings.Contains(cfg.Layer, "{layer.") {
		if target := lg.FindLayer(context.Background(), cfg.Layer); target != nil {
			ref.target = target.Metadata().Advertised
		}
	}

	return ref, nil
}

func (t Ref) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t Ref) GenerateTile(ctx context.Context, _ layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	layerName := t.resolveLayerName(ctx, tileRequest)
	newRequest := pkg.TileRequest{LayerName: layerName, Z: tileRequest.Z, X: tileRequest.X, Y: tileRequest.Y}

	depth, _ := pkg.RefDepthFromContext(ctx)
	if depth != nil && *depth >= maxRefDepth {
		return nil, fmt.Errorf("ref: maximum reference depth (%v) exceeded, likely a cycle involving layer %v", maxRefDepth, layerName)
	}

	// Coalescing makes a repeated name wait on itself until timeout instead of reaching the depth limit
	chain, _ := ctx.Value(refChainKey{}).([]string)
	if len(chain) == 0 {
		chain = []string{tileRequest.LayerName}
	}
	chain = append(slices.Clone(chain), layerName)
	if slices.Contains(chain[:len(chain)-1], layerName) {
		return nil, fmt.Errorf("ref: cycle detected: %v", strings.Join(chain, " -> "))
	}

	// A fresh context keeps the parent layer's placeholders from leaking into the child layer
	req, _ := pkg.ReqFromContext(ctx)
	newCtx := context.WithValue(pkg.NewRequestContext(req), refChainKey{}, chain)

	pkg.CopyAuthRestrictions(ctx, newCtx)

	if newDepth, ok := pkg.RefDepthFromContext(newCtx); ok && newDepth != nil && depth != nil {
		*newDepth = *depth + 1
	}

	span := trace.SpanFromContext(ctx)
	newCtx = trace.ContextWithSpan(newCtx, span)

	return t.layerGroup.RenderTile(newCtx, newRequest)
}

// Values aren't escaped since the result is only matched against layer names, never spliced into a URL
func (t Ref) resolveLayerName(ctx context.Context, tileRequest pkg.TileRequest) string {
	if !strings.Contains(t.Layer, "{layer.") {
		return t.Layer
	}

	lpm, _ := pkg.LayerPatternMatchesFromContext(ctx)

	return lyrRegex.ReplaceAllStringFunc(t.Layer, func(match string) string {
		layerVar := match[7 : len(match)-1]

		if lpm != nil {
			if val, ok := (*lpm)[layerVar]; ok {
				return val
			}
		}

		slog.Log(ctx, slog.LevelWarn, fmt.Sprintf("layer variable %v could not be resolved while serving layer %v", layerVar, tileRequest.LayerName))
		return ""
	})
}

// Built before this layer, so the target's metadata is final
func (t Ref) Metadata() layer.Description {
	return t.target
}
