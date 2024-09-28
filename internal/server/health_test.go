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

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const port = 3456

func initialize(t *testing.T, fail bool) (config.Config, *layer.LayerGroup) {
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

	cfgAll.Server.Health.Enabled = true
	cfgAll.Server.Health.Port = port
	cfgAll.Server.Health.Checks = []map[string]any{
		{
			"name":  "tile",
			"layer": "test",
			"delay": 1,
		},
	}

	cfgAll.Layers = append(cfgAll.Layers, layerCfg)
	lg, err := layer.ConstructLayerGroup(cfgAll, nil, nil, nil)
	require.NoError(t, err)

	return cfgAll, lg
}

// Make sure setup works and we get a callback that kills the server by ensuring we can do it twice
func Test_Health_Setup(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfg, lg := initialize(t, false)

	callback, err := SetupHealth(ctx, &cfg, lg)
	require.NoError(t, err)
	err = callback(ctx)
	require.NoError(t, err)

	callback, err = SetupHealth(ctx, &cfg, lg)
	require.NoError(t, err)
	err = callback(ctx)
	require.NoError(t, err)
}

func Test_Health_Success(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfg, lg := initialize(t, false)

	callback, err := SetupHealth(ctx, &cfg, lg)
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	resp, err := http.DefaultClient.Get(baseURL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	resp, err = http.DefaultClient.Head(baseURL + "/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	resp, err = http.DefaultClient.Get(baseURL + "/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	jsonByte, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	jsonMap := make(map[string]any)
	err = json.Unmarshal(jsonByte, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, "ok", jsonMap["status"], jsonMap)

	err = callback(ctx)
	require.NoError(t, err)
}

func Test_Health_Fail(t *testing.T) {
	ctx := pkg.BackgroundContext()
	cfg, lg := initialize(t, true)

	callback, err := SetupHealth(ctx, &cfg, lg)
	require.NoError(t, err)

	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)

	resp, err := http.DefaultClient.Get(baseURL + "/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	jsonByte, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	jsonMap := make(map[string]any)
	err = json.Unmarshal(jsonByte, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, "error", jsonMap["status"], jsonMap)
	assert.Equal(t, "error", jsonMap["checks"].(map[string]any)["tilegroxy:checks"].([]any)[0].(map[string]any)["status"], jsonMap)

	err = callback(ctx)
	require.NoError(t, err)
}
