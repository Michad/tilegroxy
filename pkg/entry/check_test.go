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

package tg

import (
	"bytes"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

func validConfig() config.Config {
	cfg := config.DefaultConfig()
	provider := map[string]interface{}{"name": "static", "color": "FFF"}
	cfg.Layers = []config.LayerConfig{{ID: "main", Provider: provider}}
	return cfg
}

// A caller who only wants pass/fail, not the "Valid" text or echoed config, has no other value to
// pass, so a nil writer has to error rather than panic inside fmt.Fprintln.
func Test_CheckConfig_NilWriterDoesNotPanic(t *testing.T) {
	cfg := validConfig()

	require.NotPanics(t, func() {
		err := CheckConfig(&cfg, CheckOptions{}, nil)
		require.NoError(t, err)
	})
}

func Test_CheckConfig_NilWriterWithEchoDoesNotPanic(t *testing.T) {
	cfg := validConfig()

	require.NotPanics(t, func() {
		err := CheckConfig(&cfg, CheckOptions{Echo: true}, nil)
		require.NoError(t, err)
	})
}

func Test_CheckConfig_ValidConfigWritesValid(t *testing.T) {
	cfg := validConfig()
	var buf bytes.Buffer

	err := CheckConfig(&cfg, CheckOptions{}, &buf)

	require.NoError(t, err)
	require.Contains(t, buf.String(), "Valid")
}

func Test_CheckConfig_InvalidConfigErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Error.Mode = "not-a-real-mode"
	var buf bytes.Buffer

	err := CheckConfig(&cfg, CheckOptions{}, &buf)

	require.Error(t, err)
}

// Every config serve refuses to start with belongs here, so check can't drift from serve again
func Test_CheckConfig_RejectsWhatServeRejects(t *testing.T) {
	sixty := uint(60)
	yes := true

	tests := map[string]func(*config.Config){
		"encrypt without domain": func(c *config.Config) {
			c.Server.Encrypt = &config.EncryptionConfig{Certificate: "cert"}
		},
		"cors wildcard with credentials": func(c *config.Config) {
			c.Server.CORS = config.CORSConfig{Enabled: true, WildcardOrigin: true, AllowCredentials: true}
		},
		"server cachecontrol nostore with maxage": func(c *config.Config) {
			c.Server.CacheControl = config.CacheControlConfig{Enabled: &yes, NoStore: &yes, MaxAge: &sixty}
		},
		"layer cachecontrol bad visibility": func(c *config.Config) {
			c.Layers[0].CacheControl = &config.CacheControlConfig{Enabled: &yes, Visibility: "everyone"}
		},
		"access log bad format": func(c *config.Config) {
			c.Logging.Access.Format = "nope"
		},
		"main log bad format": func(c *config.Config) {
			c.Logging.Main.Format = "nope"
		},
		"main log bad level": func(c *config.Config) {
			c.Logging.Main.Level = "nope"
		},
		"audit log bad format": func(c *config.Config) {
			c.Logging.Audit = config.AuditConfig{Enabled: true, Console: true, Format: "nope"}
		},
		"audit log without output": func(c *config.Config) {
			c.Logging.Audit = config.AuditConfig{Enabled: true, Format: config.AuditFormatJSON}
		},
		"health check on unknown layer": func(c *config.Config) {
			c.Health.Enabled = true
			c.Health.Checks = []map[string]any{{"name": "tile", "layer": "nope"}}
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			mutate(&cfg)
			var buf bytes.Buffer

			err := CheckConfig(&cfg, CheckOptions{}, &buf)

			require.Error(t, err)
			require.NotContains(t, buf.String(), "Valid")
		})
	}
}

func Test_CheckConfig_RejectsInvalidCORS(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.CORS = config.CORSConfig{Enabled: true, WildcardOrigin: true, AllowCredentials: true}

	err := CheckConfig(&cfg, CheckOptions{}, nil)

	require.Error(t, err)
}
