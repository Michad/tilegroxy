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

package sample

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

const tileSize = 256

type ProviderConfig struct {
	Color string // Six digit hex, such as 00AA88
}

// Renders a solid color tile once at startup and serves it for every request
type Provider struct {
	png []byte
}

func init() {
	layer.RegisterProvider(ProviderRegistration{})
}

type ProviderRegistration struct{}

func (ProviderRegistration) InitializeConfig() any {
	return ProviderConfig{Color: "FFFFFF"}
}

func (ProviderRegistration) Name() string {
	return "sample"
}

func (ProviderRegistration) DataType(_ any) config.DataType {
	return config.DataTypeRaster
}

func (ProviderRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(ProviderConfig)

	rgb, err := hex.DecodeString(cfg.Color)
	if err != nil || len(rgb) != 3 {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "provider.sample.color", cfg.Color)
	}

	img := image.NewRGBA(image.Rect(0, 0, tileSize, tileSize))
	fill := color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 255}

	for y := range tileSize {
		for x := range tileSize {
			img.Set(x, y, fill)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return &Provider{buf.Bytes()}, nil
}

func (p *Provider) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (p *Provider) GenerateTile(_ context.Context, _ layer.ProviderContext, t pkg.TileRequest) (*pkg.Image, error) {
	if _, err := t.GetBounds(); err != nil {
		return nil, err
	}

	return &pkg.Image{Content: p.png, ContentType: "image/png"}, nil
}
