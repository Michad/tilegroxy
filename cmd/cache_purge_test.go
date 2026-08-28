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
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CachePurgeCommand_UnsupportedCacheReportsCleanly(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	cacheCmd.ResetFlags()
	cachePurgeCmd.ResetFlags()
	initRoot()
	initCache()
	initCachePurge()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	// simple.json configures the "none" cache, which doesn't implement Purgeable.
	rootCmd.SetArgs([]string{"cache", "purge", "-c", "../examples/configurations/simple.json", "-l", "osm"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, string(out), "doesn't support purging")
	assert.Equal(t, -1, exitStatus)
}

func Test_CachePurgeCommand_MemoryCacheSupportsPurging(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	cacheCmd.ResetFlags()
	cachePurgeCmd.ResetFlags()
	initRoot()
	initCache()
	initCachePurge()

	rawConfig := `{"cache":{"name":"memory"},"layers":[{"id":"osm","provider":{"name":"proxy","url":"https://example.com/{z}/{x}/{y}.png"}}]}`

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"cache", "purge", "--raw-config", rawConfig, "-l", "osm"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, string(out), "Purged layer")
	assert.Equal(t, -1, exitStatus)
}

func Test_CachePurgeCommand_InvalidLayer(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	cacheCmd.ResetFlags()
	cachePurgeCmd.ResetFlags()
	initRoot()
	initCache()
	initCachePurge()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"cache", "purge", "-c", "../examples/configurations/simple.json", "-l", "does-not-exist"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, out)
	assert.Equal(t, 1, exitStatus)
}

func Test_CachePurgeCommand_MissingLayerFlag(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	cacheCmd.ResetFlags()
	cachePurgeCmd.ResetFlags()
	initRoot()
	initCache()
	initCachePurge()

	rootCmd.SetArgs([]string{"cache", "purge", "-c", "../examples/configurations/simple.json"})
	require.Error(t, rootCmd.Execute())
}

func Test_CachePurgeCommand_InvalidConfig(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	cacheCmd.ResetFlags()
	cachePurgeCmd.ResetFlags()
	initRoot()
	initCache()
	initCachePurge()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs([]string{"cache", "purge", "--raw-config", "not valid json", "-l", "osm"})
	require.NoError(t, rootCmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, out)
	assert.Equal(t, 1, exitStatus)
}
