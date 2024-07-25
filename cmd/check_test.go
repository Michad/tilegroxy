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
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CheckCommand_Execute(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	checkCmd.ResetFlags()
	initRoot()
	initCheck()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"config", "check", "-c", "../examples/configurations/simple.json"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "Valid\n", string(out))
	assert.Equal(t, -1, exitStatus)
}

func Test_CheckCommand_ExecuteInline(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	checkCmd.ResetFlags()
	initRoot()
	initCheck()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)

	cfg := `cache:
  name: multi
  tiers:
    - name: memory
      maxsize: 100
      ttl: 1000
    - name: disk
      path: /tmp
layers:
  - id: osm
    provider:
        name: proxy
        url: https://tile.openstreetmap.org/{z}/{x}/{y}.png
`
	rootCmd.SetArgs([]string{"config", "check", "--raw-config", cfg})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "Valid\n", string(out))
	assert.Equal(t, -1, exitStatus)
}
func Test_CheckCommand_ExecuteInlineJson(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	checkCmd.ResetFlags()
	initRoot()
	initCheck()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)

	cfg := `
	{
		"cache": {
			"name": "none"
		},
		"layers": [
			{
				"id": "osm",
				"provider": {
					"name": "proxy",
					"url": "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
				}
			}
		]
	}
`
	rootCmd.SetArgs([]string{"config", "check", "--raw-config", cfg})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "Valid\n", string(out))
	assert.Equal(t, -1, exitStatus)
}

func Test_CheckCommand_ExecuteCompare(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	checkCmd.ResetFlags()
	initRoot()
	initCheck()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)

	cfg := `cache:
  name: multi
  tiers:
    - name: memory
      maxsize: 100
      ttl: 1000
    - name: disk
      path: /tmp
layers:
  - id: osm
    provider:
        name: proxy
        url: https://tile.openstreetmap.org/{z}/{x}/{y}.png
`
	rootCmd.SetArgs([]string{"config", "check", "--raw-config", cfg, "--echo"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, -1, exitStatus)

	fmt.Println(string(out))

	var echoed map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &echoed))

	assert.Equal(t, "multi", echoed["Cache"].(map[string]interface{})["name"])
	assert.Equal(t, "osm", echoed["Layers"].([]interface{})[0].(map[string]interface{})["Id"])
}

func Test_CheckCommand_Invalid(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	checkCmd.ResetFlags()
	initRoot()
	initCheck()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	cfg := `cache:
  name: multi
  tiers:
    - name: memory
      maxsize: 100
      ttl: 1000
    - name: disk
      path: %v
                  OMG THIS ISN'T VALID!
layers:
  - id: osm
    provider:
        name: proxy
        url: https://tile.openstreetmap.org/{z}/{x}/{y}.png
`

	rootCmd.SetArgs([]string{"config", "check", "--raw-config", cfg})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEqual(t, "Valid\n", string(out))
	assert.Equal(t, 1, exitStatus)
}
