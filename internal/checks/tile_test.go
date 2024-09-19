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
	"os"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Michad/tilegroxy/internal/images"
	_ "github.com/Michad/tilegroxy/internal/providers"
)

func Test_Validate(t *testing.T) {
	cfgAll, lg, reg, cfg := initialize(t, false)
	cfg.Layer = "fake"

	_, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.Error(t, err)

	cfg.Layer = "test"

	hc, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.NoError(t, err)
	require.IsType(t, &TileCheck{}, hc)

	tc := hc.(*TileCheck)
	assert.Equal(t, uint(60), tc.Delay)
	assert.Equal(t, tc.Delay, tc.GetDelay())
	assert.Equal(t, DefaultZ, tc.Z)
	assert.Equal(t, DefaultX, tc.X)
	assert.Equal(t, DefaultY, tc.Y)

	cfg.Validation = "fake"

	_, err = reg.Initialize(cfg, lg, nil, &cfgAll)
	require.Error(t, err)
}

func Test_Base64(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfgAll, lg, reg, cfg := initialize(t, false)
	cfg.Layer = "test"
	cfg.Validation = ValidationBase64
	cfg.Result = "iVBORw0KGgoAAAANSUhEUgAAAgAAAAIACAIAAAB7GkOtAAAHIklEQVR4nOzVMREAIAzAQI7Dv+Uio0P+FWTLm5kDQM/dDgBghwEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBAlAEARBkAQJQBAEQZAECUAQBEGQBA1A8AAP//gX0HANL7JAoAAAAASUVORK5CYII="

	hc, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.NoError(t, err)
	require.IsType(t, &TileCheck{}, hc)
	tc := hc.(*TileCheck)

	err = hc.Check(ctx)
	require.NoError(t, err)

	tc.Result = "test"
	err = hc.Check(ctx)
	assert.Error(t, err)
}

func Test_File(t *testing.T) {
	ctx := pkg.BackgroundContext()
	dir := t.TempDir()
	file := dir + "/" + "tile_test_result.png"
	fileVal1, err := images.GetStaticImage("color:FFFFFF")
	require.NoError(t, err)
	err = os.WriteFile(file, *fileVal1, 0600)
	require.NoError(t, err)

	cfgAll, lg, reg, cfg := initialize(t, false)
	cfg.Layer = "test"
	cfg.Validation = ValidationFile
	cfg.Result = file

	hc, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.NoError(t, err)

	err = hc.Check(ctx)
	require.NoError(t, err)

	fileVal2, err := images.GetStaticImage("color:000")
	require.NoError(t, err)
	err = os.WriteFile(file, *fileVal2, 0600)
	require.NoError(t, err)

	err = hc.Check(ctx)
	assert.Error(t, err)
}

func Test_Success(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfgAll, lg, reg, cfg := initialize(t, false)
	cfg.Layer = "test"
	cfg.Validation = ValidationSuccess

	hc, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.NoError(t, err)
	require.IsType(t, &TileCheck{}, hc)

	err = hc.Check(ctx)
	assert.NoError(t, err)
}

func Test_SuccessFail(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfgAll, lg, reg, cfg := initialize(t, true)
	cfg.Layer = "test"
	cfg.Validation = ValidationSuccess

	hc, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.NoError(t, err)
	require.IsType(t, &TileCheck{}, hc)

	err = hc.Check(ctx)
	assert.Error(t, err)
}

func Test_ContentType(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfgAll, lg, reg, cfg := initialize(t, false)
	cfg.Layer = "test"
	cfg.Validation = ValidationContentType
	cfg.Result = "image/png"

	hc, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.NoError(t, err)
	require.IsType(t, &TileCheck{}, hc)
	tc := hc.(*TileCheck)

	err = hc.Check(ctx)
	require.NoError(t, err)

	tc.Result = "test"
	err = hc.Check(ctx)
	assert.Error(t, err)
}

func Test_Same(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfgAll, lg, reg, cfg := initialize(t, false)
	cfg.Layer = "test"
	cfg.Validation = ValidationSame

	hc, err := reg.Initialize(cfg, lg, nil, &cfgAll)
	require.NoError(t, err)
	require.IsType(t, &TileCheck{}, hc)
	tc := hc.(*TileCheck)

	err = hc.Check(ctx)
	require.NoError(t, err)

	err = hc.Check(ctx)
	require.NoError(t, err)

	tc.img = &pkg.Image{}

	err = hc.Check(ctx)
	require.Error(t, err)

	err = hc.Check(ctx)
	require.NoError(t, err)
}

func initialize(t *testing.T, fail bool) (config.Config, *layer.LayerGroup, TileCheckRegistration, TileCheckConfig) {
	cfgAll := config.DefaultConfig()

	var layerCfg config.LayerConfig

	if fail {
		layerCfg = config.LayerConfig{
			ID: "test",
			Provider: map[string]any{
				"name":   "fail",
				"onauth": true,
			},
		}
	} else {
		layerCfg = config.LayerConfig{
			ID: "test",
			Provider: map[string]any{
				"name":  "static",
				"color": "FFFFFF",
			},
		}
	}

	cfgAll.Layers = append(cfgAll.Layers, layerCfg)
	lg, err := layer.ConstructLayerGroup(cfgAll, nil, nil, nil)
	require.NoError(t, err)

	reg := TileCheckRegistration{}
	cfgAny := reg.InitializeConfig()

	require.IsType(t, TileCheckConfig{}, cfgAny)
	cfg := cfgAny.(TileCheckConfig)
	return cfgAll, lg, reg, cfg
}
