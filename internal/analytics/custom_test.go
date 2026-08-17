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

package analytics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A script that appends every event's layer to a file, so the test can observe that the script ran
// with the events it was given.
const workingScript = `
package custom

import (
	"context"
	"os"

	"tilegroxy/tilegroxy"
)

func record(ctx context.Context, events []tilegroxy.AnalyticsEvent, params map[string]interface{}, msgs tilegroxy.ErrorMessages) error {
	path := params["path"].(string)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, e := range events {
		if _, err := f.WriteString(e.LayerID + "\n"); err != nil {
			return err
		}
	}

	return nil
}
`

func testCustomConfig(t *testing.T, script string, params map[string]interface{}) CustomConfig {
	t.Helper()

	cfg := CustomConfig{Script: script}
	cfg.Params = params
	// Flush every event immediately so tests don't wait on the age trigger.
	cfg.Batch.MaxSize = 1
	cfg.Batch.MaxAge = 1

	return cfg
}

func Test_Custom_RecordsEvents(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages
	out := filepath.Join(t.TempDir(), "events.txt")

	a, err := CustomRegistration{}.Initialize(testCustomConfig(t, workingScript, map[string]interface{}{"path": out}), analytics.AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()
	require.NoError(t, a.Record(ctx, analytics.Event{LayerID: "main", Z: 1, X: 2, Y: 3}))

	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, a.(*Custom).Close(closeCtx))

	content, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(content), "main")
}

func Test_Custom_BatchesEvents(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages
	out := filepath.Join(t.TempDir(), "events.txt")

	cfg := testCustomConfig(t, workingScript, map[string]interface{}{"path": out})
	// Large enough that only Close triggers the flush, proving the script sees a whole batch.
	cfg.Batch.MaxSize = 100
	cfg.Batch.MaxAge = 600

	a, err := CustomRegistration{}.Initialize(cfg, analytics.AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()

	for range 5 {
		require.NoError(t, a.Record(ctx, analytics.Event{LayerID: "main"}))
	}

	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, a.(*Custom).Close(closeCtx))

	content, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "main\nmain\nmain\nmain\nmain\n", string(content))
}

func Test_Custom_FromFile(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "script.go")
	require.NoError(t, os.WriteFile(scriptPath, []byte(workingScript), 0600))

	out := filepath.Join(dir, "events.txt")

	cfg := CustomConfig{File: scriptPath}
	cfg.Params = map[string]interface{}{"path": out}
	cfg.Batch.MaxSize = 1

	a, err := CustomRegistration{}.Initialize(cfg, analytics.AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()
	require.NoError(t, a.Record(ctx, analytics.Event{LayerID: "fromfile"}))

	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, a.(*Custom).Close(closeCtx))

	content, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(content), "fromfile")
}

func Test_Custom_InvalidConfigurations(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	tests := []struct {
		name string
		cfg  CustomConfig
	}{
		{
			name: "neither file nor script",
			cfg:  CustomConfig{},
		},
		{
			name: "both file and script",
			cfg:  CustomConfig{File: "somewhere.go", Script: workingScript},
		},
		{
			name: "script does not compile",
			cfg:  CustomConfig{Script: "package custom\nthis is not go"},
		},
		{
			name: "script has no record function",
			cfg:  CustomConfig{Script: "package custom\n\nfunc somethingElse() {}"},
		},
		{
			name: "record has the wrong signature",
			cfg:  CustomConfig{Script: "package custom\n\nfunc record() {}"},
		},
		{
			name: "file does not exist",
			cfg:  CustomConfig{File: filepath.Join(t.TempDir(), "missing.go")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CustomRegistration{}.Initialize(test.cfg, analytics.AnalyticsDeps{ErrorMessages: msgs})
			require.Error(t, err)
		})
	}
}

func Test_Custom_ScriptErrorIsContained(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	// A script that always fails should not make Record fail: the tile was already served.
	failing := `
package custom

import (
	"context"
	"errors"

	"tilegroxy/tilegroxy"
)

func record(ctx context.Context, events []tilegroxy.AnalyticsEvent, params map[string]interface{}, msgs tilegroxy.ErrorMessages) error {
	return errors.New("nope")
}
`

	a, err := CustomRegistration{}.Initialize(testCustomConfig(t, failing, nil), analytics.AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()
	require.NoError(t, a.Record(ctx, analytics.Event{LayerID: "main"}))

	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, a.(*Custom).Close(closeCtx))
}
