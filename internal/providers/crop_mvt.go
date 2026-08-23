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
	"github.com/go-spatial/geom"
	"github.com/go-spatial/geom/encoding/mvt"
	"github.com/go-spatial/geom/planar/clip"
	"github.com/go-spatial/geom/planar/makevalid"
	"github.com/go-spatial/geom/planar/makevalid/hitmap"
	"github.com/go-spatial/geom/winding"
	"github.com/golang/protobuf/proto" //nolint:staticcheck // go-spatial/geom's mvt.Tile.VTile returns a legacy protoc-gen-go v1 message type; google.golang.org/protobuf/proto can't marshal it.
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

	if !tileBounds.Intersects(boundsToCrop) {
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

	if tileBounds.Contains(boundsToCrop) {
		slog.Log(ctx, slog.LevelDebug, "Tile fully contains crop bounds")
		return img, nil
	}

	tileProjBounds, err := tileRequest.GetBoundsProjection(pkg.SRIDPsuedoMercator)
	if err != nil {
		return nil, err
	}

	inTile, err := mvt.DecodeByte(img.Content)
	if err != nil {
		return nil, err
	}

	clipBoxPixels := boundsToPixelExtent(boundsToCrop.ConvertToPsuedoMercatorRange().ConfineToPsuedoMercatorRange(), *tileProjBounds)

	outTile, err := cropMvtTile(ctx, inTile, clipBoxPixels)
	if err != nil {
		return nil, err
	}

	vTile, err := outTile.VTile(ctx)
	if err != nil {
		return nil, err
	}

	output, err := proto.Marshal(vTile)
	if err != nil {
		return nil, err
	}

	return &pkg.Image{Content: output, ContentType: mvtContentType, ForceSkipCache: img.ForceSkipCache}, nil
}

// boundsToPixelExtent converts a crop region, in the same projection as tileBounds, into the tile-local
// pixel space (0,0)-(extent,extent) that MVT geometries are encoded in. pixelExtent should match the
// extent of the layers being cropped; mvt.DefaultExtent is used since MVT layers can each declare their
// own extent and this is only used to build the clip box, not to reproject the geometry itself.
func boundsToPixelExtent(crop pkg.Bounds, tileBounds pkg.Bounds) *geom.Extent {
	pixelExtent := float64(mvt.DefaultExtent)

	minX := (crop.West - tileBounds.West) / tileBounds.Width() * pixelExtent
	maxX := (crop.East - tileBounds.West) / tileBounds.Width() * pixelExtent
	// MVT pixel space has Y increasing downward (south), geographic bounds have North at the top.
	minY := (tileBounds.North - crop.North) / tileBounds.Height() * pixelExtent
	maxY := (tileBounds.North - crop.South) / tileBounds.Height() * pixelExtent

	return geom.NewExtent([2]float64{minX, minY}, [2]float64{maxX, maxY})
}

func cropMvtTile(ctx context.Context, inTile *mvt.Tile, clipBox *geom.Extent) (*mvt.Tile, error) {
	outTile := new(mvt.Tile)
	order := winding.Order{}

	for _, l := range inTile.Layers() {
		newLayer := new(mvt.Layer)
		newLayer.Name = l.Name
		newLayer.SetExtent(l.Extent())

		for _, f := range l.Features() {
			croppedGeo, err := cropMvtGeometry(ctx, f.Geometry, clipBox, order)
			if err != nil {
				return nil, err
			}
			if croppedGeo == nil {
				continue
			}

			newFeatures := mvt.NewFeatures(croppedGeo, f.Tags)
			for i := range newFeatures {
				newFeatures[i].ID = f.ID
			}
			newLayer.AddFeatures(newFeatures...)
		}

		if err := outTile.AddLayers(newLayer); err != nil {
			return nil, err
		}
	}

	return outTile, nil
}

func cropMvtGeometry(ctx context.Context, geo geom.Geometry, clipBox *geom.Extent, order winding.Order) (geom.Geometry, error) {
	hm, err := hitmap.New(clipBox, geo)
	if err != nil {
		return nil, err
	}

	mv := makevalid.Makevalid{Hitmap: hm, Clipper: clip.Default, Order: order}

	croppedGeo, _, err := mv.Makevalid(ctx, geo, clipBox)
	if err != nil {
		return nil, err
	}

	return croppedGeo, nil
}
