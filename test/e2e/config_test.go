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

//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_FreePort_ReturnsUsablePort(t *testing.T) {
	p := freePort(t)
	assert.Greater(t, p, 1024)
	assert.Less(t, p, 65536)
}

func Test_FreePort_DoesNotRepeat(t *testing.T) {
	first := freePort(t)
	second := freePort(t)

	assert.NotEqual(t, first, second)
}

func Test_RenderConfig_SubstitutesPorts(t *testing.T) {
	out := renderConfig(t, "server:\n  port: {{.Port}}\n  health:\n    port: {{.HealthPort}}\n", ports{Server: 1111, Health: 2222})

	assert.Contains(t, out, "port: 1111")
	assert.Contains(t, out, "port: 2222")
	assert.NotContains(t, out, "{{")
}

func Test_WriteConfig_WritesReadableYamlFile(t *testing.T) {
	path := writeConfig(t, "server:\n  port: 1\n")

	require.True(t, strings.HasSuffix(path, ".yml"))

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "server:\n  port: 1\n", string(b))
}
