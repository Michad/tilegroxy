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
	cmd.Execute()
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	var res map[string]string
	err = json.Unmarshal(out, &res)
	assert.NoError(t, err)

	assert.NotNil(t, res["version"])
	assert.NotNil(t, res["ref"])
	assert.NotNil(t, res["goVersion"])
	assert.NotNil(t, res["buildDate"])
}
