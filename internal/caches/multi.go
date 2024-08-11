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
	"context"
	"errors"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
)

type MultiConfig struct {
	Tiers []map[string]interface{}
}

type Multi struct {
	Tiers []cache.Cache
}

func init() {
	cache.RegisterCache(MultiRegistration{})
}

type MultiRegistration struct {
}

func (s MultiRegistration) InitializeConfig() any {
	return MultiConfig{}
}

func (s MultiRegistration) Name() string {
	return "multi"
}

func (s MultiRegistration) Initialize(configAny any, errorMessages config.ErrorMessages) (cache.Cache, error) {
	config := configAny.(MultiConfig)

	tierCaches := make([]cache.Cache, len(config.Tiers))

	for i, tierRawConfig := range config.Tiers {
		tierCache, err := cache.ConstructCache(tierRawConfig, errorMessages)

		if err != nil {
			return nil, err
		}

		tierCaches[i] = tierCache
	}

	return Multi{tierCaches}, nil
}

func (c Multi) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	var allErrors error

	for _, cache := range c.Tiers {
		img, err := cache.Lookup(ctx, t)
		if err != nil {
			allErrors = errors.Join(allErrors, err)
		}

		if img != nil {
			return img, allErrors
		}
	}

	return nil, allErrors
}

func (c Multi) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	var allErrors error

	for _, cache := range c.Tiers {
		err := cache.Save(ctx, t, img)
		allErrors = errors.Join(allErrors, err)
	}

	return allErrors
}
