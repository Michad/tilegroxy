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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/images"
)

type StaticConfig struct {
	Image string
}

type Static struct {
	StaticConfig
	img *internal.Image
}

func ConstructStatic(config StaticConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages) (*Static, error) {
	if config.Image == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.static.image", "")
	}

	img, err := images.GetStaticImage(config.Image)

	if err != nil {
		return nil, err
	}

	return &Static{config, img}, nil
}

func (t Static) PreAuth(authContext AuthContext) (AuthContext, error) {
	return AuthContext{Bypass: true}, nil
}

func (t Static) GenerateTile(authContext AuthContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	return t.img, nil
}
