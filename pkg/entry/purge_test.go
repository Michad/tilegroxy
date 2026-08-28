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

package tg

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_PurgeCache_MissingLayerNameErrors(t *testing.T) {
	cfg := validConfig()
	var buf bytes.Buffer

	err := PurgeCache(&cfg, PurgeOptions{}, &buf)

	require.Error(t, err)
}

func Test_PurgeCache_UnknownLayerErrors(t *testing.T) {
	cfg := validConfig()
	var buf bytes.Buffer

	err := PurgeCache(&cfg, PurgeOptions{LayerName: "does-not-exist"}, &buf)

	require.Error(t, err)
}

// The default "none" cache doesn't implement Purgeable, which must be reported cleanly rather
// than as an error - it's the expected outcome for most cache backends.
func Test_PurgeCache_UnsupportedCacheReportsCleanly(t *testing.T) {
	cfg := validConfig()
	var buf bytes.Buffer

	err := PurgeCache(&cfg, PurgeOptions{LayerName: "main"}, &buf)

	require.NoError(t, err)
	require.Contains(t, buf.String(), "doesn't support purging")
}

func Test_PurgeCache_MemoryCacheSupportsPurging(t *testing.T) {
	cfg := validConfig()
	cfg.Cache = map[string]interface{}{"name": "memory"}
	var buf bytes.Buffer

	err := PurgeCache(&cfg, PurgeOptions{LayerName: "main"}, &buf)

	require.NoError(t, err)
	require.Contains(t, buf.String(), "Purged layer")
}

func Test_PurgeCache_InvalidConfigErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Error.Mode = "not-a-real-mode"
	var buf bytes.Buffer

	err := PurgeCache(&cfg, PurgeOptions{LayerName: "main"}, &buf)

	require.Error(t, err)
}

// A caller who only wants pass/fail has no other value to pass, so a nil writer has to work
// rather than panic inside fmt.Fprintf.
func Test_PurgeCache_NilWriterDoesNotPanic(t *testing.T) {
	cfg := validConfig()

	require.NotPanics(t, func() {
		err := PurgeCache(&cfg, PurgeOptions{LayerName: "main"}, nil)
		require.NoError(t, err)
	})
}
