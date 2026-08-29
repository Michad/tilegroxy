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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_SeedCommand_Execute(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "1"})
	require.NoError(t, rootCmd.Execute())
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
	require.Error(t, rootCmd.Execute())
}

func Test_SeedCommand_InvalidLayer(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "hello"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.NotEmpty(t, out)
	assert.Equal(t, 1, exitStatus)
}

func Test_SeedCommand_InvalidThread(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "osm", "-t", "0"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.NotEmpty(t, out)
	assert.Equal(t, 1, exitStatus)
}

func Test_SeedCommand_InvalidZoom(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "2000"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.NotEmpty(t, out)
	assert.Equal(t, 1, exitStatus)
}

func Test_SeedCommand_InvalidConfig(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "--raw-config", "asfasfas", "-l", "osm"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.NotEmpty(t, out)
	assert.Equal(t, 1, exitStatus)
}

func Test_SeedCommand_ExcessTiles(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "20"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.NotEmpty(t, out)
	assert.Equal(t, 1, exitStatus)
}

// The progress file is what makes an interrupted seed resumable, so the command has to produce one
// by default and pick it back up when asked.
func Test_SeedCommand_ProgressFileAndResume(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	progressPath := filepath.Join(t.TempDir(), "progress.json")

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--verbose", "--progress", progressPath, "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "1"})
	require.NoError(t, rootCmd.Execute())
	assert.Equal(t, -1, exitStatus)

	raw, err := os.ReadFile(progressPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"position":4`)

	rootCmd.SetArgs([]string{"seed", "--verbose", "--progress", progressPath, "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "1"})
	require.NoError(t, rootCmd.Execute())
	assert.Equal(t, -1, exitStatus)
}

// A progress file recorded for a different area indexes into a different sequence of tiles.
func Test_SeedCommand_MismatchedProgressFile(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	progressPath := filepath.Join(t.TempDir(), "progress.json")

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"seed", "--progress", progressPath, "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "1"})
	require.NoError(t, rootCmd.Execute())
	require.Equal(t, -1, exitStatus)

	rootCmd.SetArgs([]string{"seed", "--progress", progressPath, "-c", "../examples/configurations/simple.json", "-l", "osm", "-z", "2"})
	require.NoError(t, rootCmd.Execute())

	out, err := io.ReadAll(b)
	require.NoError(t, err)
	assert.Contains(t, string(out), "different seed run")
	assert.Equal(t, 1, exitStatus)
}
