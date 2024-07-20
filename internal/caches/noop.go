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

package caches

import (
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
)

type NoopConfig struct {
}

type Noop struct {
	NoopConfig
}

func init() {
	entities.RegisterCache(NoopRegistration{})
}

type NoopRegistration struct {
}

func (s NoopRegistration) InitializeConfig() any {
	return NoopConfig{}
}

func (s NoopRegistration) Name() string {
	return "none"
}

func (s NoopRegistration) Initialize(configAny any, clientConfig config.ClientConfig, ErrorMessages config.ErrorMessages) (entities.Cache, error) {
	config := configAny.(NoopConfig)
	return Noop{config}, nil
}

func (c Noop) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c Noop) Save(t pkg.TileRequest, img *pkg.Image) error {
	return nil
}
