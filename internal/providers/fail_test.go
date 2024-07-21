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
	"errors"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layers"
)

var testErrMessages = config.DefaultConfig().Error.Messages
var testClientConfig = config.DefaultConfig().Client

type FailConfig struct {
	OnAuth  bool
	Message string
}

type Fail struct {
	FailConfig
}

func init() {
	layers.RegisterProvider(FailRegistration{})
}

type FailRegistration struct {
}

func (s FailRegistration) InitializeConfig() any {
	return FailConfig{}
}

func (s FailRegistration) Name() string {
	return "fail"
}

func (s FailRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *layers.LayerGroup) (layers.Provider, error) {
	config := cfgAny.(FailConfig)
	return &Fail{config}, nil
}

func (t Fail) PreAuth(ctx *pkg.RequestContext, providerContext layers.ProviderContext) (layers.ProviderContext, error) {
	if t.OnAuth {
		return providerContext, errors.New(t.Message)
	}
	return providerContext, nil
}

func (t Fail) GenerateTile(ctx *pkg.RequestContext, providerContext layers.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	return nil, errors.New(t.Message)
}
