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
	"net/http"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// How long a reload may take to become observable on the serving port
	reloadObserveTimeout = 30 * time.Second
	// Enough that a generation leak compounds into something measurable
	reloadBurstCount = 10
	// So the watcher sees each write as distinct rather than coalescing them
	reloadBurstSpacing = 300 * time.Millisecond
	// How long to wait to prove a rejected config changed nothing
	settleWindow = 3 * time.Second
	// How long the readiness observer polls across a reload
	observeWindow = 5 * time.Second
	// Sized so the observer never blocks
	blipBuffer = 128
	// A multiple rather than a delta, since the absolute count depends on what else the runtime has open
	fdGrowthFactor = 3
	// Matches writeConfig
	configMode = 0600
)

// Hot reload requires the --hot-reload flag
const reloadConfigA = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

const reloadConfigB = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
layers:
  - id: color
    provider:
      name: static
      color: "000000"
  - id: second
    provider:
      name: static
      color: "FFFFFF"
`

// Renders through Start's template path to keep the allocated port
func rewriteConfig(t *testing.T, inst *Instance, raw string) {
	t.Helper()

	rendered := renderConfig(t, raw, inst.ports)

	require.NoError(t, os.WriteFile(inst.ConfigPath, []byte(rendered), configMode))
}

// A client hitting a live socket must never see a failure across a swap
func Test_Reload_DropsNoRequests(t *testing.T) {
	inst := Start(t, Config{Raw: reloadConfigA, HotReload: true})

	load := inst.StartLoad("/tiles/color/8/12/32", loadWorkers)
	time.Sleep(loadWarmup) // Deliberately generating load before the disruption.

	rewriteConfig(t, inst, reloadConfigB)

	Until(t, reloadObserveTimeout, "the new layer to be served", func() bool {
		resp, err := http.Get(inst.BaseURL() + "/tiles/second/8/12/32")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()

		return resp.StatusCode == http.StatusOK
	})

	res := load.Stop()

	assert.True(t, res.AllOK(), "reload dropped requests: %+v. Output:\n%s", res, inst.Output())
}

// Repeated saves used to pin several generations at once, each holding its own pools
func Test_Reload_BurstOfReloadsDropsNoRequests(t *testing.T) {
	inst := Start(t, Config{Raw: reloadConfigA, HotReload: true})

	load := inst.StartLoad("/tiles/color/8/12/32", loadWorkers)

	for i := range reloadBurstCount {
		if i%2 == 0 {
			rewriteConfig(t, inst, reloadConfigB)
		} else {
			rewriteConfig(t, inst, reloadConfigA)
		}

		time.Sleep(reloadBurstSpacing) // Deliberately spacing writes so each is seen as a change.
	}

	res := load.Stop()

	assert.True(t, res.AllOK(), "reload burst dropped requests: %+v. Output:\n%s", res, inst.Output())
}

// Asserted under load so the claim is continuity rather than eventual recovery
func Test_Reload_MalformedConfigLeavesServerServing(t *testing.T) {
	inst := Start(t, Config{Raw: reloadConfigA, HotReload: true})

	load := inst.StartLoad("/tiles/color/8/12/32", loadWorkers)
	time.Sleep(loadWarmup) // Deliberately generating load before the disruption.

	require.NoError(t, os.WriteFile(inst.ConfigPath, []byte("asfasfasfasflkasfjaslfjlasasfjlkafkf"), configMode))

	time.Sleep(settleWindow) // Deliberately holding the window open to prove nothing broke.

	res := load.Stop()

	assert.True(t, res.AllOK(), "a malformed config disrupted serving: %+v", res)

	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)
}

// A different path from malformed YAML, where a partial swap is likeliest. A ref to a missing layer fails construction
func Test_Reload_UnconstructableConfigLeavesServerServing(t *testing.T) {
	inst := Start(t, Config{Raw: reloadConfigA, HotReload: true})

	load := inst.StartLoad("/tiles/color/8/12/32", loadWorkers)
	time.Sleep(loadWarmup) // Deliberately generating load before the disruption.

	rewriteConfig(t, inst, `
server:
  port: {{.Port}}
  drainDelay: 0
layers:
  - id: broken
    provider:
      name: ref
      layer: nosuchlayer
`)

	time.Sleep(settleWindow) // Deliberately holding the window open to prove nothing broke.

	res := load.Stop()

	assert.True(t, res.AllOK(), "an unconstructable config disrupted serving: %+v", res)

	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)
}

// The draining flag read under healthMutex in buildShutdownPhases exists for exactly this race
func Test_Reload_RacingShutdownStaysClean(t *testing.T) {
	for range 3 {
		inst := Start(t, Config{Raw: reloadConfigA, HotReload: true})

		rewriteConfig(t, inst, reloadConfigB)
		inst.Signal(syscall.SIGTERM)

		code := inst.WaitExit(exitTimeout)

		assert.Equal(t, 0, code)
		assert.NotContains(t, inst.Output(), "panic")
	}
}

// An orchestrator polling /health must never see a blip. health_reload_test.go covers the rebuild itself
func Test_Reload_HealthChecksRebuildWithoutReadinessBlip(t *testing.T) {
	healthA := `
server:
  port: {{.Port}}
  drainDelay: 0
health:
  enabled: true
  port: {{.HealthPort}}
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`
	healthB := `
server:
  port: {{.Port}}
  drainDelay: 0
health:
  enabled: true
  port: {{.HealthPort}}
  checks:
    - name: tile
      layer: color
      z: 8
      x: 12
      y: 32
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	inst := Start(t, Config{Raw: healthA, HotReload: true})

	stop := make(chan struct{})
	blips := make(chan int, blipBuffer)

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}

			resp, err := http.Get(inst.HealthURL() + "/health")
			if err == nil {
				if resp.StatusCode != http.StatusOK {
					select {
					case blips <- resp.StatusCode:
					default:
					}
				}

				_ = resp.Body.Close()
			}

			time.Sleep(pollInterval)
		}
	}()

	rewriteConfig(t, inst, healthB)
	time.Sleep(observeWindow) // Deliberately observing across the reload window.
	close(stop)

	select {
	case code := <-blips:
		t.Errorf("readiness blipped to %v during a health check rebuild", code)
	default:
	}
}

// A leak of retired generations shows up as file descriptors that never come back down
func Test_Reload_DoesNotLeakFileDescriptors(t *testing.T) {
	inst := Start(t, Config{Raw: reloadConfigA, HotReload: true})

	fdCount := func() int {
		entries, err := os.ReadDir("/proc/" + strconv.Itoa(inst.cmd.Process.Pid) + "/fd")
		if err != nil {
			t.Skipf("cannot read /proc fd table, skipping on this platform: %v", err)
		}

		return len(entries)
	}

	before := fdCount()

	for i := range reloadBurstCount {
		if i%2 == 0 {
			rewriteConfig(t, inst, reloadConfigB)
		} else {
			rewriteConfig(t, inst, reloadConfigA)
		}

		time.Sleep(reloadBurstSpacing) // Deliberately spacing writes so each is seen as a change.
	}

	time.Sleep(observeWindow) // Deliberately allowing retired generations to close.

	after := fdCount()

	assert.Less(t, after, before*fdGrowthFactor,
		"file descriptors grew from %v to %v across ten reloads, suggesting generations are not being released", before, after)
}
