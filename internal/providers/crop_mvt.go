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
	"errors"
	"fmt"
	"log/slog"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
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
	Primary       layer.Provider
	errorMessages config.ErrorMessages
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

func (s CropMvtRegistration) DataType(_ any) config.DataType {
	return config.DataTypeMVT
}

func (s CropMvtRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(CropMvtConfig)

	primary, err := constructCropPrimary(cfg.Primary, deps, "provider.cropmvt.primary", config.DataTypeMLT)
	if err != nil {
		return nil, err
	}

	return &CropMvt{cfg, primary, deps.ErrorMessages}, nil
}

// Clipping the other vector format would fail on every tile not wholly inside the bounds
func constructCropPrimary(rawConfig map[string]interface{}, deps layer.ProviderDeps, path string, rejected config.DataType) (layer.Provider, error) {
	primary, err := layers.ConstructProvider(rawConfig, deps)
	if err != nil {
		return nil, err
	}

	if err = checkForInvalidDataType(primary, rejected, path, deps.ErrorMessages); err != nil {
		return nil, errors.Join(err, layer.CloseProvider(context.Background(), primary))
	}

	return primary, nil
}

func (t CropMvt) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return t.Primary.PreAuth(ctx, providerContext)
}

func (t CropMvt) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	c := vectorCrop{t.Bounds, t.BoundsFromAuth, mvtContentType, "cropmvt.img", t.errorMessages}

	return c.generate(ctx, t.Primary, providerContext, tileRequest, clipMvt)
}

func clipMvt(_ context.Context, content []byte, boundsToCrop pkg.Bounds, tileRequest pkg.TileRequest) ([]byte, error) {
	collection, err := mvt.Unmarshal(content)
	if err != nil {
		return nil, err
	}

	tile := maptile.New(uint32(tileRequest.X), uint32(tileRequest.Y), maptile.Zoom(tileRequest.Z)) //#nosec G115 -- tileRequest coordinates are already range-checked by GetBounds before clipping

	collection.ProjectToWGS84(tile)
	collection.Clip(boundsToOrbBound(boundsToCrop))
	collection.ProjectToTile(tile)

	return mvt.Marshal(collection)
}

// Shared by every vector format, leaving only the clip itself to the format
type vectorCrop struct {
	bounds         pkg.Bounds
	boundsFromAuth bool
	contentType    string
	imgParam       string
	errorMessages  config.ErrorMessages
}

type clipFunc func(ctx context.Context, content []byte, boundsToCrop pkg.Bounds, tileRequest pkg.TileRequest) ([]byte, error)

func (c vectorCrop) generate(ctx context.Context, primary layer.Provider, providerContext layer.ProviderContext, tileRequest pkg.TileRequest, clip clipFunc) (*pkg.Image, error) {
	boundsToCrop := c.bounds

	if c.boundsFromAuth {
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
		return &pkg.Image{Content: []byte{}, ContentType: c.contentType, ForceSkipCache: true}, nil
	}

	img, err := primary.GenerateTile(ctx, providerContext, tileRequest)
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf(c.errorMessages.ParamRequired, c.imgParam)
	}

	if boundsToCrop.IsNullIsland() {
		return img, nil
	}

	if boundsToCrop.Contains(*tileBounds) {
		slog.Log(ctx, slog.LevelDebug, "Tile fully contained by crop bounds")
		return img, nil
	}

	output, err := clip(ctx, img.Content, boundsToCrop, tileRequest)
	if err != nil {
		return nil, err
	}

	return &pkg.Image{Content: output, ContentType: c.contentType, ForceSkipCache: img.ForceSkipCache}, nil
}

func boundsToOrbBound(b pkg.Bounds) orb.Bound {
	return orb.Bound{
		Min: orb.Point{b.West, b.South},
		Max: orb.Point{b.East, b.North},
	}
}

func (t CropMvt) Metadata() layer.Description {
	return clipToCrop(layer.DescribeTree(t.Primary), t.Bounds, t.BoundsFromAuth)
}

func (t CropMvt) Children() []layer.Provider {
	return []layer.Provider{t.Primary}
}

func (s CropMvtRegistration) WrapBounds(inner layer.Provider, bounds pkg.Bounds, deps layer.ProviderDeps) (layer.Provider, error) {
	return &CropMvt{CropMvtConfig{Bounds: bounds}, inner, deps.ErrorMessages}, nil
}
