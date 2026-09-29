// Copyright 2026 Michael Davis
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
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type CompositeMLTConfig struct {
	Providers []map[string]interface{}
}

func init() {
	layer.RegisterProvider(CompositeMLTRegistration{})
}

type CompositeMLTRegistration struct {
}

func (s CompositeMLTRegistration) InitializeConfig() any {
	return CompositeMLTConfig{}
}

func (s CompositeMLTRegistration) Name() string {
	return "compositemlt"
}

func (s CompositeMLTRegistration) DataType(_ any) config.DataType {
	return config.DataTypeMLT
}

func (s CompositeMLTRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(CompositeMLTConfig)

	return newCompositeVector(cfg.Providers, deps, "provider.compositemlt.providers", config.DataTypeMVT, mltContentType)
}
