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
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConfigFlagsCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	c := &cobra.Command{}
	c.Flags().String("config", "./tilegroxy.yml", "")
	c.Flags().String("raw-config", "", "")
	c.Flags().String("remote-provider", "", "")
	c.Flags().String("remote-endpoint", "http://127.0.0.1:2379", "")
	c.Flags().String("remote-path", "/config/tilegroxy.yml", "")
	c.Flags().String("remote-type", "yaml", "")
	c.Flags().Bool(reloadFlag, false, "")
	require.NoError(t, c.ParseFlags(args))

	return c
}

func Test_ReloadSource_RereadsFile(t *testing.T) {
	source, err := reloadSourceFromCommand(newConfigFlagsCommand(t, "--config", "../examples/configurations/simple.json"))
	require.NoError(t, err)
	require.NotNil(t, source)

	cfg, err := source()
	require.NoError(t, err)
	require.Len(t, cfg.Layers, 1)
	assert.Equal(t, "osm", cfg.Layers[0].ID)
}

func Test_ReloadSource_RereadsRemote(t *testing.T) {
	source, err := reloadSourceFromCommand(newConfigFlagsCommand(t, "--remote-provider", "not-a-provider"))
	require.NoError(t, err)
	require.NotNil(t, source)

	_, err = source()
	assert.Error(t, err, "the remote provider should be used rather than the default config file")
}

func Test_ReloadSource_RereadsRaw(t *testing.T) {
	source, err := reloadSourceFromCommand(newConfigFlagsCommand(t, "--raw-config", `{"layers":[{"id":"raw","provider":{"name":"static","color":"FFFFFF"}}]}`))
	require.NoError(t, err)
	require.NotNil(t, source)

	cfg, err := source()
	require.NoError(t, err)
	require.Len(t, cfg.Layers, 1)
	assert.Equal(t, "raw", cfg.Layers[0].ID)
}

func Test_ReloadSource_ErrorsWithoutConfigFlags(t *testing.T) {
	_, err := reloadSourceFromCommand(&cobra.Command{})

	assert.Error(t, err)
}
