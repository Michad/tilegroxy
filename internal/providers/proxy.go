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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type ProxyConfig struct {
	URL     string
	InvertY bool // Used for TMS
	Srid    uint
}

type Proxy struct {
	ProxyConfig
	clientConfig config.ClientConfig
}

func init() {
	layer.RegisterProvider(ProxyRegistration{})
}

type ProxyRegistration struct {
}

func (s ProxyRegistration) InitializeConfig() any {
	return ProxyConfig{}
}

func (s ProxyRegistration) Name() string {
	return "proxy"
}

func (s ProxyRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, _ *layer.LayerGroup, _ *datastore.DatastoreRegistry) (layer.Provider, error) {
	cfg := cfgAny.(ProxyConfig)
	if cfg.URL == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.proxy.url", "")
	}

	if cfg.Srid == 0 {
		cfg.Srid = pkg.SRIDWGS84
	}
	if cfg.Srid != pkg.SRIDWGS84 && cfg.Srid != pkg.SRIDPsuedoMercator {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.proxy.srid", cfg.Srid, []int{pkg.SRIDPsuedoMercator, pkg.SRIDWGS84})
	}

	return &Proxy{cfg, clientConfig}, nil
}

func (t Proxy) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t Proxy) GenerateTile(ctx context.Context, _ layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	url, err := replaceURLPlaceholders(ctx, tileRequest, t.URL, t.InvertY, t.Srid)
	if err != nil {
		return nil, err
	}

	return getTile(ctx, t.clientConfig, url, make(map[string]string))
}
