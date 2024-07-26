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
	"fmt"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type StaticConfig struct {
	Image string
	Color string
}

type Static struct {
	StaticConfig
	img *pkg.Image
}

func init() {
	layer.RegisterProvider(StaticRegistration{})
}

type StaticRegistration struct {
}

func (s StaticRegistration) InitializeConfig() any {
	return StaticConfig{}
}

func (s StaticRegistration) Name() string {
	return "static"
}

func (s StaticRegistration) Initialize(cfgAny any, _ config.ClientConfig, errorMessages config.ErrorMessages, _ *layer.LayerGroup) (layer.Provider, error) {
	cfg := cfgAny.(StaticConfig)
	if cfg.Image == "" {
		if cfg.Color != "" {
			cfg.Image = images.KeyPrefixColor + cfg.Color
		} else {
			return nil, fmt.Errorf(errorMessages.OneOfRequired, []string{"provider.static.image", "provider.static.color"})
		}
	}

	img, err := images.GetStaticImage(cfg.Image)

	if err != nil {
		return nil, err
	}

	return &Static{cfg, img}, nil
}

func (t Static) PreAuth(_ *pkg.RequestContext, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t Static) GenerateTile(_ *pkg.RequestContext, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return t.img, nil
}
