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
	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type FallbackConfig struct {
	Primary   map[string]interface{}
	Secondary map[string]interface{}
}

type Fallback struct {
	Primary  *Provider
	Secondary *Provider
}

func ConstructFallback(config FallbackConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, primary *Provider, secondary *Provider) (*Fallback, error) {
	return &Fallback{primary, secondary}, nil
}

func (t Fallback) PreAuth(authContext AuthContext) (AuthContext, error) {
	return (*t.Primary).PreAuth(authContext)
}

func (t Fallback) GenerateTile(authContext AuthContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	img, err := (*t.Primary).GenerateTile(authContext, tileRequest)

	if err != nil {
		return (*t.Secondary).GenerateTile(authContext, tileRequest)
	}

	return img, err
}
