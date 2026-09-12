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

package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runDiffCommand(t *testing.T, args ...string) string {
	t.Helper()

	exitStatus = -1
	rootCmd.ResetFlags()
	diffCmd.ResetFlags()
	initRoot()
	initDiff()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs(append([]string{"config", "diff"}, args...))
	require.NoError(t, rootCmd.Execute())

	out, err := io.ReadAll(b)
	require.NoError(t, err)

	return string(out)
}

func writeDiffConfig(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	return path
}

const diffConfigA = `
server:
  port: 8080
cache:
  name: memory
  maxsize: 100
layers:
  - id: osm
    provider:
      name: static
      color: FFF
`

const diffConfigB = `
server:
  port: 9090
cache:
  name: memory
  maxsize: 500
layers:
  - id: osm
    provider:
      name: static
      color: "000"
  - id: extra
    provider:
      name: static
      color: FFF
`

func Test_DiffCommand_IdenticalConfigs(t *testing.T) {
	path := writeDiffConfig(t, "same.yml", diffConfigA)

	out := runDiffCommand(t, "-c", path, path)

	assert.Contains(t, out, "No differences between the two configurations.")
	assert.Equal(t, -1, exitStatus)
}

func Test_DiffCommand_ReportsGroupedChanges(t *testing.T) {
	pathA := writeDiffConfig(t, "a.yml", diffConfigA)
	pathB := writeDiffConfig(t, "b.yml", diffConfigB)

	out := runDiffCommand(t, "-c", pathA, pathB)

	assert.Contains(t, out, "Reload-able Changes")
	assert.Contains(t, out, "Changes Requiring a Restart")
	assert.Contains(t, out, "extra")
	assert.Equal(t, 1, exitStatus)
}

func Test_DiffCommand_ColorizesByDefault(t *testing.T) {
	pathA := writeDiffConfig(t, "a.yml", diffConfigA)
	pathB := writeDiffConfig(t, "b.yml", diffConfigB)

	assert.Contains(t, runDiffCommand(t, "-c", pathA, pathB), "\033[")
	assert.NotContains(t, runDiffCommand(t, "--no-color", "-c", pathA, pathB), "\033[")
}

func Test_DiffCommand_JSONFormat(t *testing.T) {
	pathA := writeDiffConfig(t, "a.yml", diffConfigA)
	pathB := writeDiffConfig(t, "b.yml", diffConfigB)

	out := runDiffCommand(t, "-c", pathA, "--json", pathB)

	var res map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Contains(t, res, "reload")
	assert.Contains(t, res, "restart")
	assert.Equal(t, 1, exitStatus)
}

func Test_DiffCommand_YAMLFormat(t *testing.T) {
	pathA := writeDiffConfig(t, "a.yml", diffConfigA)
	pathB := writeDiffConfig(t, "b.yml", diffConfigB)

	out := runDiffCommand(t, "-c", pathA, "--yaml", pathB)

	assert.Contains(t, out, "reload:")
	assert.Equal(t, 1, exitStatus)
}

func Test_DiffCommand_TableFormat(t *testing.T) {
	pathA := writeDiffConfig(t, "a.yml", diffConfigA)
	pathB := writeDiffConfig(t, "b.yml", diffConfigB)

	out := runDiffCommand(t, "-c", pathA, "--table", pathB)

	assert.Contains(t, out, "Can Reload")
	assert.Contains(t, out, "Change Kind")
	assert.Contains(t, out, "layers.extra")
	assert.Contains(t, out, "server.port")
	assert.Equal(t, 1, exitStatus)
}

func Test_DiffCommand_MarkdownFormat(t *testing.T) {
	pathA := writeDiffConfig(t, "a.yml", diffConfigA)
	pathB := writeDiffConfig(t, "b.yml", diffConfigB)

	out := runDiffCommand(t, "-c", pathA, "--markdown", pathB)

	assert.Contains(t, out, "| Can Reload | Change Kind | Path | Value |")
	assert.Contains(t, out, "| --- | --- | --- | --- |")
	assert.Contains(t, out, "| yes | added | layers.extra |")
	assert.Contains(t, out, "| no | modified | server.port |")
	assert.Equal(t, 1, exitStatus)
}

func Test_DiffCommand_FormatFlagsAreMutuallyExclusive(t *testing.T) {
	path := writeDiffConfig(t, "a.yml", diffConfigA)

	exitStatus = -1
	rootCmd.ResetFlags()
	diffCmd.ResetFlags()
	initRoot()
	initDiff()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"config", "diff", "-c", path, "--json", "--yaml", path})

	require.Error(t, rootCmd.Execute())
}

func Test_DiffCommand_MissingComparisonFile(t *testing.T) {
	path := writeDiffConfig(t, "a.yml", diffConfigA)

	out := runDiffCommand(t, "-c", path, filepath.Join(t.TempDir(), "nope.yml"))

	assert.Contains(t, out, "Invalid configuration")
	assert.Equal(t, 1, exitStatus)
}

func Test_DiffCommand_MissingBaseConfig(t *testing.T) {
	path := writeDiffConfig(t, "a.yml", diffConfigA)

	out := runDiffCommand(t, "-c", filepath.Join(t.TempDir(), "nope.yml"), path)

	assert.Contains(t, out, "Invalid configuration")
	assert.Equal(t, 1, exitStatus)
}
