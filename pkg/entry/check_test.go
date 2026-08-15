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
