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

package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ExecuteVersionCommand(t *testing.T) {
	rootCmd.ResetFlags()
	versionCmd.ResetFlags()
	initRoot()
	initVersion()

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"version", "--json"})
	require.NoError(t, cmd.Execute())

	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	var res map[string]string
	err = json.Unmarshal(out, &res)
	require.NoError(t, err)

	assert.NotEmpty(t, res["version"])
	assert.NotEmpty(t, res["ref"])
	assert.NotEmpty(t, res["goVersion"])
	assert.NotEmpty(t, res["buildDate"])
}

func Test_ExecuteVersionCommandShort(t *testing.T) {
	rootCmd.ResetFlags()
	versionCmd.ResetFlags()
	initRoot()
	initVersion()

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"version", "--json", "--short"})
	require.NoError(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	var res map[string]string
	err = json.Unmarshal(out, &res)
	require.NoError(t, err)

	assert.NotEmpty(t, res["version"])
	assert.Empty(t, res["ref"])
	assert.Empty(t, res["goVersion"])
	assert.Empty(t, res["buildDate"])
}

func Test_ExecuteVersionCommandShortNoJson(t *testing.T) {
	rootCmd.ResetFlags()
	versionCmd.ResetFlags()
	initRoot()
	initVersion()

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"version", "--short"})
	require.NoError(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, out)
}
func Test_ExecuteVersionCommandNoJson(t *testing.T) {
	rootCmd.ResetFlags()
	versionCmd.ResetFlags()
	initRoot()
	initVersion()

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"version"})
	require.NoError(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, out)
}
