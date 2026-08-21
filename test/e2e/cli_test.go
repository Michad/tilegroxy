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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CLI_VersionShortExitsZero(t *testing.T) {
	out, code := Run(t, "version", "--short")

	assert.Equal(t, 0, code)
	assert.NotEmpty(t, out)
}

func Test_CLI_HelpExitsZero(t *testing.T) {
	out, code := Run(t, "--help")

	assert.Equal(t, 0, code)
	assert.Contains(t, out, "tilegroxy")
}

// Validation lives under the config command as `config check`, not a top-level `check`.
func Test_CLI_CheckAcceptsValidConfig(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`)

	out, code := Run(t, "config", "check", "-c", path)

	assert.Equal(t, 0, code, "output: %s", out)
	assert.Contains(t, out, "Valid")
}

func Test_CLI_CheckRejectsInvalidConfig(t *testing.T) {
	path := writeConfig(t, "asfasfasfasflkasfjaslfjlasasfjlkafkf")

	out, code := Run(t, "config", "check", "-c", path)

	assert.NotEqual(t, 0, code)
	// check reports "Invalid configuration", not the "Error" prefix that serve uses.
	assert.Contains(t, out, "Invalid configuration")
}

// A container orchestrator sees only the exit code, so that is what these assert, rather than the
// exitStatus package global the in-process tests read.
func Test_CLI_MissingConfigExitsNonZero(t *testing.T) {
	out, code := Run(t, "serve", "-c", filepath.Join(t.TempDir(), "nope.yml"))

	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "Error")
}

func Test_CLI_MalformedConfigExitsNonZero(t *testing.T) {
	path := writeConfig(t, "asfasfasfasflkasfjaslfjlasasfjlkafkf")

	out, code := Run(t, "serve", "-c", path)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "Error")
}

// Errors go to stdout, not stderr: cmd/serve.go uses rootCmd.OutOrStdout() on every error path.
// This pins that behavior so a future change to it is a deliberate decision.
func Test_CLI_ErrorsAreWrittenToStdout(t *testing.T) {
	path := writeConfig(t, "asfasfasfasflkasfjaslfjlasasfjlkafkf")

	cmd := exec.CommandContext(t.Context(), BinaryPath(t), "serve", "-c", path)

	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderrPipe, err := cmd.StderrPipe()
	require.NoError(t, err)

	require.NoError(t, cmd.Start())

	stdout, err := io.ReadAll(stdoutPipe)
	require.NoError(t, err)
	stderr, err := io.ReadAll(stderrPipe)
	require.NoError(t, err)

	_ = cmd.Wait()

	assert.Contains(t, string(stdout), "Error")
	assert.NotContains(t, string(stderr), "Error")
}

func Test_CLI_PortAlreadyInUseExitsNonZero(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	// Reuse the running instance's config, so the second process collides on its port.
	b, err := os.ReadFile(inst.ConfigPath)
	require.NoError(t, err)

	second := writeConfig(t, string(b))

	out, code := Run(t, "serve", "-c", second)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "Error")
}
