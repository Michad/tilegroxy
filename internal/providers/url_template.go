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
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type UrlTemplateConfig struct {
	Template string
}

type UrlTemplate struct {
	UrlTemplateConfig
	clientConfig  *config.ClientConfig
	errorMessages *config.ErrorMessages
}

func ConstructUrlTemplate(config UrlTemplateConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages) (*UrlTemplate, error) {
	return &UrlTemplate{config, clientConfig, errorMessages}, nil
}

func (t UrlTemplate) PreAuth(authContext AuthContext) (AuthContext, error) {
	return AuthContext{Bypass: true}, nil
}

func (t UrlTemplate) GenerateTile(authContext AuthContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	if t.Template == "" {
		return nil, fmt.Errorf(t.errorMessages.InvalidParam, "provider.url template.url", "")
	}

	b, err := tileRequest.GetBounds()

	if err != nil {
		return nil, err
	}

	//width, height (in pixels), srs (in PROJ.4 format), xmin, ymin, xmax, ymax (in projected map units), and zoom
	url := strings.ReplaceAll(t.Template, "$xmin", fmt.Sprintf("%f", b.MinLong))
	url = strings.ReplaceAll(url, "$xmax", fmt.Sprintf("%f", b.MaxLong))
	url = strings.ReplaceAll(url, "$ymin", fmt.Sprintf("%f", b.MinLat))
	url = strings.ReplaceAll(url, "$ymax", fmt.Sprintf("%f", b.MaxLat))
	url = strings.ReplaceAll(url, "$zoom", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "$width", "256") //TODO: allow these being dynamic
	url = strings.ReplaceAll(url, "$height", "256")
	url = strings.ReplaceAll(url, "$srs", "4326") //TODO: decide if I want this to be dynamic

	return getTile(t.clientConfig, url, make(map[string]string))
}
