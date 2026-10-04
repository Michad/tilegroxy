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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// By then the live generation has either survived or been torn down
	healthFailureTimeout = 30 * time.Second
	// Outlasts the batcher's maxAge so a working batcher would have flushed at least once
	analyticsObserveWindow = 5 * time.Second
	// One tile request after the reload proves whether the batcher still accepts
	analyticsBatchMaxSize = 1
	// The watcher can miss a write landing too close to startup
	watcherSettle = 2 * time.Second
	// Before rewriting the config again
	rewriteRetryWindow = 5 * time.Second
)

// On the generation still serving traffic, this is the symptom of #862
const batcherClosedLog = "analytics batcher is closed"

// Emitted when the rebuild fails, which triggers the faulty close
const healthRebuildFailedLog = "Failed to rebuild health subsystem on reload"

// A closed Batcher logs rejected events, unlike silent static providers or memory caches, making teardown observable
const reloadAnalyticsScript = `package custom

import (
	"os"
	"strconv"

	"tilegroxy/tilegroxy"
)

func record(ctx tilegroxy.Context, events []tilegroxy.AnalyticsEvent, params map[string]interface{}, msgs tilegroxy.ErrorMessages) error {
	f, err := os.OpenFile(params["path"].(string), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, e := range events {
		if _, err := f.WriteString(e.LayerID + " " + strconv.Itoa(e.Z) + "\n"); err != nil {
			return err
		}
	}

	return nil
}
`

// The server port stays templated for Start, but the health port is literal since it's one the test itself holds
func reloadHealthFailureConfig(healthPort string, scriptPath, eventPath string) string {
	return fmt.Sprintf(`
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
health:
  enabled: true
  port: %s
cache:
  name: none
analytics:
  name: custom
  file: %s
  path: %s
  batch:
    maxSize: %d
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`, healthPort, scriptPath, eventPath, analyticsBatchMaxSize)
}

// Health failing to bind is the practical trigger, unlike a bad check name which aborts before any swap
func occupyPort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig

	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err, "cannot bind a port to block")

	t.Cleanup(func() { _ = l.Close() })

	addr, ok := l.Addr().(*net.TCPAddr)
	require.Truef(t, ok, "listener address is %T, not a TCP address", l.Addr())

	return addr.Port
}

// The watcher occasionally misses a write, which would otherwise make these tests flaky
func rewriteUntilCondition(t *testing.T, inst *Instance, raw, desc string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(Scale(healthFailureTimeout))

	for time.Now().Before(deadline) {
		rewriteConfig(t, inst, raw)

		windowEnd := time.Now().Add(Scale(rewriteRetryWindow))
		for time.Now().Before(windowEnd) {
			if cond() {
				return
			}

			time.Sleep(pollInterval)
		}
	}

	t.Fatalf("config was rewritten repeatedly but never saw %s. Output:\n%s", desc, inst.Output())
}

// For the common case of waiting on a log line
func rewriteUntilReloaded(t *testing.T, inst *Instance, raw, awaited string) {
	t.Helper()

	rewriteUntilCondition(t, inst, raw, strconv.Quote(awaited), func() bool {
		return strings.Contains(inst.Output(), awaited)
	})
}

// Regression test for #862: a failed health rebuild closed the live generation. Tiles still 200, so analytics is what's asserted
func Test_Reload_FailedHealthRebuildKeepsLiveEntitiesOpen(t *testing.T) {
	dir := t.TempDir()
	scriptPath := dir + "/analytics.go"
	eventPath := dir + "/events.log"

	require.NoError(t, os.WriteFile(scriptPath, []byte(reloadAnalyticsScript), configMode))

	inst := Start(t, Config{
		Raw:       reloadHealthFailureConfig("{{.HealthPort}}", scriptPath, eventPath),
		HotReload: true,
	})

	// A baseline so missing events later mean the reload broke analytics
	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)

	Until(t, healthFailureTimeout, "the first analytics event to be written", func() bool {
		b, err := os.ReadFile(eventPath)

		return err == nil && len(b) > 0
	})

	baseline, err := os.ReadFile(eventPath)
	require.NoError(t, err)

	blockedPort := occupyPort(t)

	time.Sleep(watcherSettle) // Deliberately letting the watcher settle before rewriting.

	// Only the health port changes, so any analytics difference is from the failed rebuild alone
	rewriteUntilReloaded(t, inst,
		reloadHealthFailureConfig(strconv.Itoa(blockedPort), scriptPath, eventPath), healthRebuildFailedLog)

	// The bug isn't an outage but the silent loss of everything else the live generation owns
	deadline := time.Now().Add(Scale(analyticsObserveWindow))
	for time.Now().Before(deadline) {
		inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)
		time.Sleep(pollInterval)
	}

	// Counted so a failure reports the number rejected instead of reprinting all output
	rejected := strings.Count(inst.Output(), batcherClosedLog)

	assert.Zerof(t, rejected,
		"a failed health rebuild closed the analytics batcher of the generation still serving traffic: %v events were rejected as \"%s\"",
		rejected, batcherClosedLog)

	after, err := os.ReadFile(eventPath)
	require.NoError(t, err)

	assert.Greaterf(t, len(after), len(baseline),
		"no analytics events were recorded after a failed health rebuild, so the live generation's entities were torn down. Recorded %v bytes before and %v after",
		len(baseline), len(after))
}

// A later good config must bring health and analytics back regardless of how the failure was handled
func Test_Reload_RecoversAfterFailedHealthRebuild(t *testing.T) {
	dir := t.TempDir()
	scriptPath := dir + "/analytics.go"
	eventPath := dir + "/events.log"

	require.NoError(t, os.WriteFile(scriptPath, []byte(reloadAnalyticsScript), configMode))

	inst := Start(t, Config{
		Raw:       reloadHealthFailureConfig("{{.HealthPort}}", scriptPath, eventPath),
		HotReload: true,
	})

	blockedPort := occupyPort(t)

	time.Sleep(watcherSettle) // Deliberately letting the watcher settle before rewriting.

	rewriteUntilReloaded(t, inst,
		reloadHealthFailureConfig(strconv.Itoa(blockedPort), scriptPath, eventPath), healthRebuildFailedLog)

	// Health must rebind and analytics must record again
	recoveredPort := testutil.FreePort(t)

	rewriteUntilCondition(t, inst,
		reloadHealthFailureConfig(strconv.Itoa(recoveredPort), scriptPath, eventPath),
		"health to answer on the new port", func() bool {
			resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(recoveredPort) + "/health")
			if err != nil {
				return false
			}
			defer func() { _ = resp.Body.Close() }()

			return resp.StatusCode == http.StatusOK
		})

	// May not exist yet since the script creates it on its first batch
	before, err := os.ReadFile(eventPath)
	if err != nil {
		require.ErrorIs(t, err, os.ErrNotExist)

		before = nil
	}

	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)

	Until(t, healthFailureTimeout, "analytics to record again after recovery", func() bool {
		after, readErr := os.ReadFile(eventPath)

		return readErr == nil && len(after) > len(before)
	})
}
