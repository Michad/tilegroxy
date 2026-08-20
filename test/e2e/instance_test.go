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
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shared config for tests that are not about draining. drainDelay defaults to 5 seconds, which
// every test would otherwise spend waiting during cleanup; the shutdown tests set it explicitly.
const staticLayerConfig = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

func Test_Instance_StartsAndServesTiles(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	resp, err := http.Get(inst.BaseURL() + "/tiles/color/8/12/32")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func Test_Instance_CapturesOutput(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	assert.Contains(t, inst.Output(), "Binding")
}

func Test_Instance_SigtermExitsZero(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	inst.Signal(syscall.SIGTERM)

	assert.Equal(t, 0, inst.WaitExit(30*time.Second))
}

func Test_Run_VersionSubcommandExitsZero(t *testing.T) {
	out, code := Run(t, "version")

	assert.Equal(t, 0, code)
	assert.NotEmpty(t, out)
}
