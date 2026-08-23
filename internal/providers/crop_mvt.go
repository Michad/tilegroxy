// Copyright 2025 Michael Davis
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
	"log/slog"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/maptile"
)

type CropMvtConfig struct {
	Primary        map[string]interface{}
	Bounds         pkg.Bounds
	BoundsFromAuth bool
}

type CropMvt struct {
	CropMvtConfig
	Primary layer.Provider
}

func init() {
	layer.RegisterProvider(CropMvtRegistration{})
}

type CropMvtRegistration struct {
}

func (s CropMvtRegistration) InitializeConfig() any {
	return CropMvtConfig{}
}

func (s CropMvtRegistration) Name() string {
	return "cropmvt"
}

func (s CropMvtRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(CropMvtConfig)

	primary, err := layer.ConstructProvider(cfg.Primary, deps)
	if err != nil {
		return nil, err
	}

	return &CropMvt{cfg, primary}, nil
}

func (t CropMvt) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return t.Primary.PreAuth(ctx, providerContext)
}

func (t CropMvt) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	boundsToCrop := t.Bounds

	if t.BoundsFromAuth {
		b, ok := pkg.AllowedAreaFromContext(ctx)
		if ok && b != nil && !b.IsNullIsland() {
			boundsToCrop = *b
		}
	}

	tileBounds, err := tileRequest.GetBounds()
	if err != nil {
		return nil, err
	}

	if !boundsToCrop.IsNullIsland() && !tileBounds.Intersects(boundsToCrop) {
		slog.Log(ctx, slog.LevelDebug, "Tile fully outside crop bounds")
		return &pkg.Image{Content: []byte{}, ContentType: mvtContentType, ForceSkipCache: true}, nil
	}

	img, err := t.Primary.GenerateTile(ctx, providerContext, tileRequest)
	if err != nil {
		return nil, err
	}

	if boundsToCrop.IsNullIsland() {
		return img, nil
	}

	if boundsToCrop.Contains(*tileBounds) {
		slog.Log(ctx, slog.LevelDebug, "Tile fully contained by crop bounds")
		return img, nil
	}

	layers, err := mvt.Unmarshal(img.Content)
	if err != nil {
		return nil, err
	}

	tile := maptile.New(uint32(tileRequest.X), uint32(tileRequest.Y), maptile.Zoom(tileRequest.Z)) //#nosec G115 -- tileRequest coordinates are already range-checked by GetBounds above

	layers.ProjectToWGS84(tile)
	layers.Clip(boundsToOrbBound(boundsToCrop))
	layers.ProjectToTile(tile)

	output, err := mvt.Marshal(layers)
	if err != nil {
		return nil, err
	}

	return &pkg.Image{Content: output, ContentType: mvtContentType, ForceSkipCache: img.ForceSkipCache}, nil
}

func (t CropMvt) DataType() pkg.DataType {
	return pkg.DataTypeMVT
}

func boundsToOrbBound(b pkg.Bounds) orb.Bound {
	return orb.Bound{
		Min: orb.Point{b.West, b.South},
		Max: orb.Point{b.East, b.North},
	}
}
