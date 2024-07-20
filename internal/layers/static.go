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

package layers

import (
	"fmt"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type StaticConfig struct {
	Image string
	Color string
}

type Static struct {
	StaticConfig
	img *pkg.Image
}

func ConstructStatic(config StaticConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (*Static, error) {
	if config.Image == "" {
		if config.Color != "" {
			config.Image = images.KeyPrefixColor + config.Color
		} else {
			return nil, fmt.Errorf(errorMessages.OneOfRequired, []string{"provider.static.image", "provider.static.color"})
		}
	}

	img, err := images.GetStaticImage(config.Image)

	if err != nil {
		return nil, err
	}

	return &Static{config, img}, nil
}

func (t Static) PreAuth(ctx *pkg.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return ProviderContext{AuthBypass: true}, nil
}

func (t Static) GenerateTile(ctx *pkg.RequestContext, providerContext ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	return t.img, nil
}
