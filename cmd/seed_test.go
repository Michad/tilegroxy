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
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_SeedCommand_Execute(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "1"})
	assert.Nil(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.Greater(t, len(out), 69)
	assert.Equal(t, -1, exitStatus)
}

func Test_SeedCommand_MissingArgs(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json"})
	assert.NotNil(t, rootCmd.Execute())
}

func Test_SeedCommand_InvalidLayer(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "hello"})
	assert.Nil(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.Greater(t, len(out), 0)
	assert.Equal(t, 1, exitStatus)
}

func Test_SeedCommand_ExcessTiles(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOutput(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "20"})
	assert.Nil(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.Greater(t, len(out), 0)
	assert.Equal(t, 1, exitStatus)
}
