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
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CreateCommand_Execute(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	createCmd.ResetFlags()
	initRoot()
	initCreate()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"config", "create"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, out)
	assert.Equal(t, -1, exitStatus)
}
func Test_CreateCommand_ExecuteJson(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	createCmd.ResetFlags()
	initRoot()
	initCreate()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"config", "create", "--json"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, out)
	assert.Equal(t, -1, exitStatus)
}

func Test_CreateCommand_ExecuteYml(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	createCmd.ResetFlags()
	initRoot()
	initCreate()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"config", "create", "--yaml"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, out)
	assert.Equal(t, -1, exitStatus)
}

func Test_CreateCommand_ExecuteJsonFile(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	createCmd.ResetFlags()
	initRoot()
	initCreate()

	fil, err := os.CreateTemp(os.TempDir(), "*-test-create.json")
	require.NoError(t, err)
	fil.Close()
	defer os.Remove(fil.Name())

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"config", "create", "-o", fil.Name()})
	require.NoError(t, rootCmd.Execute())
	_, err = io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	c, err := os.ReadFile(fil.Name())

	require.NoError(t, err)
	assert.NotEmpty(t, c)
	assert.Equal(t, -1, exitStatus)
}
func Test_CreateCommand_ExecuteYmlFile(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	createCmd.ResetFlags()
	initRoot()
	initCreate()

	fil, err := os.CreateTemp(os.TempDir(), "*-test-create.yml")
	require.NoError(t, err)
	fil.Close()
	defer os.Remove(fil.Name())

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"config", "create", "--default", "--yaml", "-o", fil.Name()})
	require.NoError(t, rootCmd.Execute())
	_, err = io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	c, err := os.ReadFile(fil.Name())

	require.NoError(t, err)
	assert.NotEmpty(t, c)
	assert.Equal(t, -1, exitStatus)
}
