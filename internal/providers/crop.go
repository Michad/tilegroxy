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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"log/slog"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/anthonynsimon/bild/transform"
)

type CropConfig struct {
	Primary        map[string]interface{}
	Secondary      map[string]interface{}
	Bounds         pkg.Bounds
	BoundsFromAuth bool
}

type Crop struct {
	CropConfig
	Primary   layer.Provider
	Secondary layer.Provider
}

func init() {
	layer.RegisterProvider(CropRegistration{})
}

type CropRegistration struct {
}

func (s CropRegistration) InitializeConfig() any {
	return CropConfig{}
}

func (s CropRegistration) Name() string {
	return "crop"
}

func (s CropRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *layer.LayerGroup, datastores *datastore.DatastoreRegistry) (layer.Provider, error) {
	cfg := cfgAny.(CropConfig)

	primary, err := layer.ConstructProvider(cfg.Primary, clientConfig, errorMessages, layerGroup, datastores)
	if err != nil {
		return nil, err
	}
	secondary, err := layer.ConstructProvider(cfg.Secondary, clientConfig, errorMessages, layerGroup, datastores)
	if err != nil {
		return nil, err
	}

	return &Crop{cfg, primary, secondary}, nil
}

func (t Crop) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return t.Primary.PreAuth(ctx, providerContext)
}

func (t Crop) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	boundsToCrop := t.Bounds

	if t.BoundsFromAuth {
		b, ok := pkg.AllowedAreaFromContext(ctx)
		if ok && b != nil && !b.IsNullIsland() {
			boundsToCrop = *b
		}
	}

	intersects, err := tileRequest.IntersectsBounds(boundsToCrop)

	if err != nil {
		return nil, err
	}

	if !boundsToCrop.IsNullIsland() && !intersects {
		slog.Log(ctx, slog.LevelDebug, "Image fully outside crop bounds")
		return t.Secondary.GenerateTile(ctx, providerContext, tileRequest)
	}

	img, err := t.Primary.GenerateTile(ctx, providerContext, tileRequest)
	if err != nil {
		return nil, err
	}

	if boundsToCrop.IsNullIsland() {
		return img, nil
	}

	tileBounds, err := tileRequest.GetBoundsProjection(pkg.SRIDPsuedoMercator)
	if err != nil {
		return nil, err
	}

	img2, err := t.Secondary.GenerateTile(ctx, providerContext, tileRequest)
	if err != nil {
		return nil, err
	}

	realImage, _, err := image.Decode(bytes.NewReader(img.Content))
	if err != nil {
		return nil, err
	}

	realImage2, _, err := image.Decode(bytes.NewReader(img2.Content))
	if err != nil {
		return nil, err
	}

	resizeImages(ctx, realImage, realImage2)
	resultImage := image.NewRGBA(realImage.Bounds())
	width := resultImage.Bounds().Dx()
	height := resultImage.Bounds().Dy()

	pixelWidth := tileBounds.Width() / float64(width)
	pixelHeight := tileBounds.Height() / float64(height)

	boundsToCropPsuedo := boundsToCrop.ConvertToPsuedoMercatorRange()

	for i := range width * height {
		x := i % width
		y := i / width

		lng := (float64(x)/float64(width))*tileBounds.Width() + tileBounds.West + pixelWidth/2
		lat := tileBounds.North - (float64(y)/float64(height))*tileBounds.Height() - pixelHeight/2
		slog.Log(ctx, config.LevelAbsurd, fmt.Sprintf("Pixel at %v, %v translates to %v, %v", x, y, lng, lat))

		if boundsToCropPsuedo.ContainsPoint(lng, lat) {
			slog.Log(ctx, config.LevelAbsurd, fmt.Sprintf("Pixel at %v, %v going to primary", x, y))
			resultImage.Set(x, y, realImage.At(x, y))
		} else {
			slog.Log(ctx, config.LevelAbsurd, fmt.Sprintf("Pixel at %v, %v going to secondary (%v)", x, y, boundsToCropPsuedo))
			resultImage.Set(x, y, realImage2.At(x, y))
		}
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err = png.Encode(writer, resultImage)

	if err != nil {
		return nil, err
	}

	err = writer.Flush()

	if err != nil {
		return nil, err
	}
	output := buf.Bytes()

	return &pkg.Image{Content: output, ContentType: mimePng, ForceSkipCache: img.ForceSkipCache}, nil
}

func resizeImages(ctx context.Context, img image.Image, img2 image.Image) {
	if img.Bounds() != img2.Bounds() {
		var size image.Point
		if img.Bounds().Max.X > img2.Bounds().Max.X {
			size.X = img.Bounds().Max.X
		} else {
			size.X = img2.Bounds().Max.X
		}

		if img.Bounds().Max.Y > img2.Bounds().Max.Y {
			size.Y = img.Bounds().Max.Y
		} else {
			size.Y = img2.Bounds().Max.Y
		}

		if img.Bounds().Max != size {
			slog.DebugContext(ctx, fmt.Sprintf("Resizing image 1 from %v to %v", img.Bounds().Max, size))
			img = transform.Resize(img, size.X, size.Y, transform.NearestNeighbor)
		}
		if img2.Bounds().Max != size {
			slog.DebugContext(ctx, fmt.Sprintf("Resizing image 2 from %v to %v", img2.Bounds().Max, size))
			img2 = transform.Resize(img2, size.X, size.Y, transform.NearestNeighbor)
		}

	}
}
