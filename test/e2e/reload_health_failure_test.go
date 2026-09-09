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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// How long to wait for the failed health rebuild to be logged, which is the point after which
	// the live generation has either survived or been torn down.
	healthFailureTimeout = 30 * time.Second
	// How long to keep requesting tiles after the failed rebuild. Long enough to outlast the
	// batcher's maxAge so a working batcher would have flushed at least once.
	analyticsObserveWindow = 5 * time.Second
	// The batcher writes on every event, so a single tile request after the reload is enough to
	// prove whether the batcher is still accepting.
	analyticsBatchMaxSize = 1
	// How long to let a freshly started instance settle before rewriting its config. The watcher
	// can miss a write that lands too close to startup.
	watcherSettle = 2 * time.Second
	// How long to wait for one config rewrite to be picked up before writing it again.
	rewriteRetryWindow = 5 * time.Second
)

// The log the wrapper emits when a request reaches a batcher that has already been closed. Its
// presence on the generation still serving traffic is the symptom of #862.
const batcherClosedLog = "analytics batcher is closed"

// The log healthReloader emits when the rebuild fails, which is what triggers the faulty close.
const healthRebuildFailedLog = "Failed to rebuild health subsystem on reload"

// A custom analytics script is what makes the teardown observable from outside the process: it
// owns a Batcher, and a closed Batcher rejects events with a log rather than failing silently the
// way a static provider or a memory cache does.
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

// reloadHealthFailureConfig renders the config for this scenario. The server port stays a template
// placeholder so Start substitutes the port it allocated and waits on, while the health port is
// written literally: the whole point is pointing it at a port the test itself holds, which is not
// one the harness allocates.
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

// occupyPort binds a port and holds it for the life of the test, so a reload pointing health at it
// fails to bind. This is the trigger the issue identifies as the practical one, health failing to
// claim its listener, as opposed to a bad check name which aborts before any swap.
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

// rewriteUntilCondition rewrites the config and waits for cond, rewriting again if it has not held
// by the end of a window. The config watcher occasionally misses a single write, which would
// otherwise make these tests flaky rather than failing for the reason they are about.
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

// rewriteUntilReloaded is rewriteUntilCondition for the common case of waiting on a log line.
func rewriteUntilReloaded(t *testing.T, inst *Instance, raw, awaited string) {
	t.Helper()

	rewriteUntilCondition(t, inst, raw, strconv.Quote(awaited), func() bool {
		return strings.Contains(inst.Output(), awaited)
	})
}

// Reproduces #862. A reload whose health rebuild fails runs strictly after the handler swap has
// already retired the old generation, so the error propagates to a close path that tears down the
// entities now serving traffic. Tiles keep returning 200 throughout, which is why the assertion is
// on analytics continuing to accept events rather than on the tile status.
func Test_Reload_FailedHealthRebuildKeepsLiveEntitiesOpen(t *testing.T) {
	dir := t.TempDir()
	scriptPath := dir + "/analytics.go"
	eventPath := dir + "/events.log"

	require.NoError(t, os.WriteFile(scriptPath, []byte(reloadAnalyticsScript), configMode))

	inst := Start(t, Config{
		Raw:       reloadHealthFailureConfig("{{.HealthPort}}", scriptPath, eventPath),
		HotReload: true,
	})

	// Confirm the baseline before disrupting anything, so a later absence of events means the
	// reload broke analytics rather than analytics never having worked.
	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)

	Until(t, healthFailureTimeout, "the first analytics event to be written", func() bool {
		b, err := os.ReadFile(eventPath)

		return err == nil && len(b) > 0
	})

	baseline, err := os.ReadFile(eventPath)
	require.NoError(t, err)

	blockedPort := occupyPort(t)

	time.Sleep(watcherSettle) // Deliberately letting the watcher settle before rewriting.

	// Only the health port changes. Everything the tile path depends on is identical, so any
	// difference in analytics afterwards is attributable to the failed health rebuild alone.
	rewriteUntilReloaded(t, inst,
		reloadHealthFailureConfig(strconv.Itoa(blockedPort), scriptPath, eventPath), healthRebuildFailedLog)

	// The server is expected to keep serving tiles. The bug is not an outage, it is the silent
	// loss of everything else the live generation owns.
	deadline := time.Now().Add(Scale(analyticsObserveWindow))
	for time.Now().Before(deadline) {
		inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)
		time.Sleep(pollInterval)
	}

	// Counted rather than asserted against the log itself, so a failure reports how many events
	// were rejected instead of reprinting the entire captured output.
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

// Recovery is part of the contract the issue documents: a later good config must bring health and
// analytics back regardless of how the failed reload was handled.
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

	// Point health back at a port nothing holds. The health subsystem has to rebind and analytics
	// has to record again.
	recoveredPort := freePort(t)

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

	// The file may not exist yet: no tile has necessarily been served in this test before now, and
	// the script only creates it on its first batch.
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
