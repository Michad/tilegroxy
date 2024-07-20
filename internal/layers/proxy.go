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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type ProxyConfig struct {
	Url     string
	InvertY bool //Used for TMS
}

type Proxy struct {
	ProxyConfig
	clientConfig config.ClientConfig
}

func ConstructProxy(config ProxyConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (*Proxy, error) {
	if config.Url == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.proxy.url", "")
	}

	return &Proxy{config, clientConfig}, nil
}

func (t Proxy) PreAuth(ctx *pkg.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return ProviderContext{AuthBypass: true}, nil
}

func (t Proxy) GenerateTile(ctx *pkg.RequestContext, providerContext ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	url, err := replaceUrlPlaceholders(ctx, tileRequest, t.Url, t.InvertY)
	if err != nil {
		return nil, err
	}

	return getTile(ctx, t.clientConfig, url, make(map[string]string))
}
