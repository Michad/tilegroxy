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
	"math"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type ProxyConfig struct {
	Url     string
	InvertY bool //Used for TMS
}

type Proxy struct {
	ProxyConfig
	clientConfig  *config.ClientConfig
	errorMessages *config.ErrorMessages
}

func ConstructProxy(config ProxyConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages) (*Proxy, error) {
	return &Proxy{config, clientConfig, errorMessages}, nil
}

func (t Proxy) PreAuth(authContext *AuthContext) error {
	return nil
}

func (t Proxy) GenerateTile(authContext *AuthContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	if t.Url == "" {
		return nil, fmt.Errorf(t.errorMessages.InvalidParam, "provider.proxy.url", "")
	}

	y := tileRequest.Y
	if t.InvertY {
		y = int(math.Exp2(float64(tileRequest.Z))) - y - 1
	}

	url := strings.ReplaceAll(t.Url, "{Z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{Y}", strconv.Itoa(y))
	url = strings.ReplaceAll(url, "{y}", strconv.Itoa(y))
	url = strings.ReplaceAll(url, "{X}", strconv.Itoa(tileRequest.X))
	url = strings.ReplaceAll(url, "{x}", strconv.Itoa(tileRequest.X))

	return getTile(t.clientConfig, url, make(map[string]string))
}
