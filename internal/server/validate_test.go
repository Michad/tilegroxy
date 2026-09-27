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

package server

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ValidateConfig(t *testing.T) {
	sixty := uint(60)
	yes := true

	tests := map[string]struct {
		mutate func(*config.Config)
		errMsg string
	}{
		"default": {
			mutate: func(_ *config.Config) {},
		},
		"encrypt without domain": {
			mutate: func(c *config.Config) { c.Server.Encrypt = &config.EncryptionConfig{Certificate: "cert"} },
			errMsg: "server.encrypt.domain",
		},
		"cors wildcard with credentials": {
			mutate: func(c *config.Config) {
				c.Server.CORS = config.CORSConfig{Enabled: true, WildcardOrigin: true, AllowCredentials: true}
			},
			errMsg: "server.cors",
		},
		"cachecontrol nostore with maxage": {
			mutate: func(c *config.Config) {
				c.Server.CacheControl = config.CacheControlConfig{Enabled: &yes, NoStore: &yes, MaxAge: &sixty}
			},
			errMsg: "server.cachecontrol.maxage",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			test.mutate(&cfg)

			err := ValidateConfig(&cfg)

			if test.errMsg == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errMsg)
		})
	}
}

func Test_ListenAndServe_RejectsInvalidCacheControl(t *testing.T) {
	sixty := uint(60)
	yes := true
	cfg := config.DefaultConfig()
	cfg.Server.CacheControl = config.CacheControlConfig{Enabled: &yes, NoStore: &yes, MaxAge: &sixty}

	require.Error(t, ListenAndServe(&cfg, nil, nil, nil))
}

func Test_ValidateHealthChecks(t *testing.T) {
	cfg := config.DefaultConfig()
	ent := &entities.Entities{LayerGroup: &layer.LayerGroup{}}
	cfg.Health.Checks = []map[string]any{{"name": "tile", "layer": "nope"}}

	require.NoError(t, ValidateHealthChecks(&cfg, ent), "disabled health should skip checks")

	cfg.Health.Enabled = true
	require.Error(t, ValidateHealthChecks(&cfg, ent))

	cfg.Health.Checks = nil
	require.NoError(t, ValidateHealthChecks(&cfg, ent))
}
