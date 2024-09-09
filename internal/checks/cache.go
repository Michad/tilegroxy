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

package checks

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/health"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

var cacheReq = pkg.TileRequest{LayerName: "___hc___", Z: 0, X: 0, Y: 0}

type CacheCheckConfig struct {
	Delay uint
}

func (s CacheCheckConfig) GetDelay() uint {
	return s.Delay
}

type CacheCheck struct {
	CacheCheckConfig
	cache         cache.Cache
	errorMessages config.ErrorMessages
}

func init() {
	health.RegisterHealthCheck(CacheCheckRegistration{})
}

type CacheCheckRegistration struct {
}

func (s CacheCheckRegistration) InitializeConfig() health.HealthCheckConfig {
	return CacheCheckConfig{}
}

func (s CacheCheckRegistration) Name() string {
	return "cache"
}

func (s CacheCheckRegistration) Initialize(checkConfig health.HealthCheckConfig, lg *layer.LayerGroup, cache cache.Cache, allCfg *config.Config) (health.HealthCheck, error) {
	cfg := checkConfig.(CacheCheckConfig)

	if cfg.Delay == 0 {
		cfg.Delay = 600
	}

	return &CacheCheck{cfg, cache, allCfg.Error.Messages}, nil
}

func makeImage() (pkg.Image, error) {
	col := strconv.FormatUint(rand.Uint64N(0xFFFFFF), 16)
	if len(col) < 6 {
		col = strings.Repeat("0", 6-len(col)) + col
	}

	img, err := images.GetStaticImage("color:" + col)

	if err != nil {
		return pkg.Image{}, err
	}

	return pkg.Image{Content: *img}, nil
}

func (h CacheCheck) Check(ctx context.Context) error {
	img, err := makeImage()

	if err != nil {
		return err
	}

	err = h.cache.Save(ctx, cacheReq, &img)

	if err != nil {
		return err
	}

	img2, err := h.cache.Lookup(ctx, cacheReq)

	if err != nil {
		return err
	}

	if img2 == nil || !slices.Equal(img.Content, img2.Content) {
		return errors.New("cache returned wrong result")
	}

	return nil
}
