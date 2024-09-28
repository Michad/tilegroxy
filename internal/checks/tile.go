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
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/health"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

const (
	ValidationSame        = "same"
	ValidationContentType = "content-tye"
	ValidationBase64      = "base-64"
	ValidationFile        = "file"
	ValidationSuccess     = "success"
	DefaultX              = 123
	DefaultY              = 534
	DefaultZ              = 10
)

var AllValidationModes = []string{ValidationSame, ValidationContentType, ValidationBase64, ValidationFile, ValidationSuccess}

type TileCheckConfig struct {
	Delay      uint
	Layer      string
	Z          int
	Y          int
	X          int
	Validation string
	Result     string
}

func (s TileCheckConfig) GetDelay() uint {
	return s.Delay
}

type TileCheck struct {
	TileCheckConfig
	lg            *layer.LayerGroup
	errorMessages config.ErrorMessages
	req           pkg.TileRequest
	img           *pkg.Image
}

func init() {
	health.RegisterHealthCheck(TileCheckRegistration{})
}

type TileCheckRegistration struct {
}

func (s TileCheckRegistration) InitializeConfig() health.HealthCheckConfig {
	return TileCheckConfig{}
}

func (s TileCheckRegistration) Name() string {
	return "tile"
}

func (s TileCheckRegistration) Initialize(checkConfig health.HealthCheckConfig, lg *layer.LayerGroup, _ cache.Cache, allCfg *config.Config) (health.HealthCheck, error) {
	cfg := checkConfig.(TileCheckConfig)

	if cfg.Delay == 0 {
		cfg.Delay = 60
	}
	if cfg.Z == 0 {
		cfg.Z = DefaultZ
	}
	if cfg.X == 0 {
		cfg.X = DefaultX
	}
	if cfg.Y == 0 {
		cfg.Y = DefaultY
	}

	if cfg.Validation == "" {
		cfg.Validation = ValidationSame
	}

	if !slices.Contains(AllValidationModes, cfg.Validation) {
		return nil, fmt.Errorf(allCfg.Error.Messages.EnumError, "check.validation", cfg.Validation, AllValidationModes)
	}

	if lg.FindLayer(pkg.BackgroundContext(), cfg.Layer) == nil {
		return nil, fmt.Errorf(allCfg.Error.Messages.EnumError, "check.layer", cfg.Layer, lg.ListLayerIDs())
	}

	req := pkg.TileRequest{LayerName: cfg.Layer, Z: cfg.Z, X: cfg.X, Y: cfg.Y}

	_, err := req.GetBounds()

	if err != nil {
		return nil, err
	}

	return &TileCheck{cfg, lg, allCfg.Error.Messages, req, nil}, nil
}

func (h *TileCheck) Check(ctx context.Context) error {
	img, err := h.lg.RenderTileNoCache(ctx, h.req)

	if err != nil {
		return err
	}

	switch h.Validation {
	case ValidationSame:
		return h.ValidateSame(ctx, img)
	case ValidationContentType:
		return h.ValidateContentType(ctx, img)
	case ValidationBase64:
		return h.ValidateBase64(ctx, img)
	case ValidationFile:
		return h.ValidateFile(ctx, img)
	}

	return nil
}

func (h *TileCheck) ValidateSame(_ context.Context, img *pkg.Image) error {
	if h.img == nil {
		h.img = img
		return nil
	}

	if img.ContentType != h.img.ContentType || !slices.Equal(img.Content, h.img.Content) {
		h.img = img
		return errors.New("result changed")
	}

	return nil
}

func (h *TileCheck) ValidateContentType(_ context.Context, img *pkg.Image) error {
	if img.ContentType != h.Result {
		return fmt.Errorf(h.errorMessages.InvalidParam, "content type", img.ContentType)
	}

	return nil
}

func (h *TileCheck) ValidateBase64(_ context.Context, img *pkg.Image) error {
	imgEncode := base64.StdEncoding.EncodeToString(img.Content)
	if imgEncode != h.Result {
		return fmt.Errorf(h.errorMessages.InvalidParam, "content", imgEncode)
	}

	return nil
}

func (h *TileCheck) ValidateFile(_ context.Context, img *pkg.Image) error {
	expected, err := os.ReadFile(h.Result)

	if err != nil {
		return err
	}

	if !slices.Equal(expected, img.Content) {
		return errors.New("result changed")
	}

	return nil
}
