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
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type URLTemplateConfig struct {
	Template string
	Width    uint16
	Height   uint16
	Srid     uint
}

type URLTemplate struct {
	Proxy
}

func (t URLTemplate) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func init() {
	layer.RegisterProvider(URLTemplateRegistration{})
}

type URLTemplateRegistration struct {
}

func (s URLTemplateRegistration) InitializeConfig() any {
	return URLTemplateConfig{}
}

func (s URLTemplateRegistration) Name() string {
	return "url template"
}

func (s URLTemplateRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, _ *layer.LayerGroup, _ *datastore.DatastoreRegistry) (layer.Provider, error) {
	cfg := cfgAny.(URLTemplateConfig)
	if cfg.Template == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.url template.url", "")
	}

	if cfg.Height == 0 {
		cfg.Height = 256
	}

	if cfg.Width == 0 {
		cfg.Width = 256
	}

	if cfg.Srid == 0 {
		cfg.Srid = pkg.SRIDWGS84
	}
	if cfg.Srid != pkg.SRIDWGS84 && cfg.Srid != pkg.SRIDPsuedoMercator {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.url template.srid", cfg.Srid, []int{pkg.SRIDPsuedoMercator, pkg.SRIDWGS84})
	}

	url := strings.ReplaceAll(cfg.Template, "$xmin", "{xmin}")
	url = strings.ReplaceAll(url, "$xmax", "{xmax}")
	url = strings.ReplaceAll(url, "$ymin", "{ymin}")
	url = strings.ReplaceAll(url, "$ymax", "{ymax}")
	url = strings.ReplaceAll(url, "$zoom", "{z}")
	url = strings.ReplaceAll(url, "$width", strconv.Itoa(int(cfg.Width)))
	url = strings.ReplaceAll(url, "$height", strconv.Itoa(int(cfg.Height)))
	url = strings.ReplaceAll(url, "$srs", strconv.FormatUint(uint64(cfg.Srid), 10))

	proxyCfg := ProxyConfig{
		URL: url,
		Srid: cfg.Srid,
	}

	return &URLTemplate{Proxy{proxyCfg, clientConfig}}, nil
}
