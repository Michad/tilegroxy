// Copyright 2026 Michael Davis
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

	"github.com/Michad/tilegroxy/internal/mlt"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/paulmach/orb"
)

type CropMltConfig struct {
	Primary        map[string]interface{}
	Bounds         pkg.Bounds
	BoundsFromAuth bool
}

type CropMlt struct {
	CropMltConfig
	Primary       layer.Provider
	errorMessages config.ErrorMessages
}

func init() {
	layer.RegisterProvider(CropMltRegistration{})
}

type CropMltRegistration struct {
}

func (s CropMltRegistration) InitializeConfig() any {
	return CropMltConfig{}
}

func (s CropMltRegistration) Name() string {
	return "cropmlt"
}

func (s CropMltRegistration) DataType(_ any) config.DataType {
	return config.DataTypeMLT
}

func (s CropMltRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(CropMltConfig)

	primary, err := constructCropPrimary(cfg.Primary, deps, "provider.cropmlt.primary", config.DataTypeMVT)
	if err != nil {
		return nil, err
	}

	return &CropMlt{cfg, primary, deps.ErrorMessages}, nil
}

func (t CropMlt) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return t.Primary.PreAuth(ctx, providerContext)
}

func (t CropMlt) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	c := vectorCrop{t.Bounds, t.BoundsFromAuth, mltContentType, "cropmlt.img", t.errorMessages}

	return c.generate(ctx, t.Primary, providerContext, tileRequest, clipMlt)
}

// Layers newer than MLT v1 can't be clipped, so they're dropped rather than leak data outside the bounds.
func clipMlt(ctx context.Context, content []byte, boundsToCrop pkg.Bounds, tileRequest pkg.TileRequest) ([]byte, error) {
	layers, skipped, err := mlt.Decode(content)
	if err != nil {
		return nil, err
	}

	if skipped > 0 {
		slog.Log(ctx, slog.LevelDebug, fmt.Sprintf("Dropped %v MLT layers of an unsupported version while cropping", skipped))
	}

	tile, err := tileRequest.GetBoundsProjection(pkg.SRIDPsuedoMercator)
	if err != nil {
		return nil, err
	}

	crop := boundsToCrop.ConvertToPsuedoMercatorRange().ConfineToPsuedoMercatorRange()
	kept := make([]mlt.Layer, 0, len(layers))

	for _, l := range layers {
		clipped := l.Clip(tileSpaceBound(crop, *tile, float64(l.Extent)))
		if clipped.FeatureCount() > 0 {
			kept = append(kept, clipped)
		}
	}

	return mlt.Encode(kept)
}

// Converts web mercator bounds into a tile's own coordinates, where y grows southward from the tile's north edge.
func tileSpaceBound(b pkg.Bounds, tile pkg.Bounds, extent float64) orb.Bound {
	scaleX := extent / (tile.East - tile.West)
	scaleY := extent / (tile.North - tile.South)

	return orb.Bound{
		Min: orb.Point{(b.West - tile.West) * scaleX, (tile.North - b.North) * scaleY},
		Max: orb.Point{(b.East - tile.West) * scaleX, (tile.North - b.South) * scaleY},
	}
}

func (t CropMlt) Metadata() layer.Description {
	return clipToCrop(layer.DescribeTree(t.Primary), t.Bounds, t.BoundsFromAuth)
}

func (t CropMlt) Children() []layer.Provider {
	return []layer.Provider{t.Primary}
}

func (s CropMltRegistration) WrapBounds(inner layer.Provider, bounds pkg.Bounds, deps layer.ProviderDeps) (layer.Provider, error) {
	return &CropMlt{CropMltConfig{Bounds: bounds}, inner, deps.ErrorMessages}, nil
}
