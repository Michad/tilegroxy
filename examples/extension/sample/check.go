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

package sample

import (
	"context"
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/health"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type CheckConfig struct {
	Delay uint
	Layer string
}

func (c CheckConfig) GetDelay() uint {
	return c.Delay
}

// Check reports healthy while a layer can still render a tile, bypassing the cache
type Check struct {
	CheckConfig
	layers layer.LayerGroup
}

func init() {
	health.RegisterHealthCheck(CheckRegistration{})
}

type CheckRegistration struct{}

func (CheckRegistration) InitializeConfig() health.HealthCheckConfig {
	return CheckConfig{Delay: 60}
}

func (CheckRegistration) Name() string {
	return "sample"
}

func (CheckRegistration) Initialize(cfgAny health.HealthCheckConfig, deps health.HealthCheckDeps) (health.HealthCheck, error) {
	cfg := cfgAny.(CheckConfig)
	errorMessages := deps.AllConfig.Error.Messages

	if !deps.LayerGroup.HasLayer(pkg.BackgroundContext(), cfg.Layer) {
		return nil, fmt.Errorf(errorMessages.EnumError, "check.sample.layer", cfg.Layer, deps.LayerGroup.ListLayerIDs())
	}

	return &Check{cfg, deps.LayerGroup}, nil
}

func (c *Check) Check(ctx context.Context) error {
	img, err := c.layers.RenderTileNoCache(ctx, pkg.TileRequest{LayerName: c.Layer})
	if err != nil {
		return err
	}

	if img == nil || len(img.Content) == 0 {
		return fmt.Errorf("layer %v rendered an empty tile", c.Layer)
	}

	return nil
}
