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

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
)

func ConfigToEntities(cfg config.Config) (*LayerGroup, entities.Authentication, error) {
	cache, err := caches.ConstructCache(cfg.Cache, cfg.Error.Messages)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing cache: %v", err)
	}

	auth, err := authentication.ConstructAuth(cfg.Authentication, cfg.Error.Messages)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing auth: %v", err)
	}

	layerGroup, err := ConstructLayerGroup(cfg, cfg.Layers, cache)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing layers: %v", err)
	}

	return layerGroup, auth, err
}
