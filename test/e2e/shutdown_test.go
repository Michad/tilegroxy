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
	"io"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Generous since a drain window plus a slow provider is the point of several tests
	exitTimeout = 45 * time.Second
	// How long a readiness transition may take to become observable
	drainObserveTimeout = 10 * time.Second
	// Keeps a connection in flight at all times without saturating a laptop
	loadWorkers = 4
	// So the disruption lands on a busy server
	loadWarmup = time.Second
	// Longer than any configured provider sleep so a client timeout never masks a server reset
	inFlightClientTimeout = 60 * time.Second
	// Well above the 2s budget and well below the 30s the provider takes if the budget is ignored
	shutdownCeiling = 20 * time.Second
	// What os/exec reports for a process killed by a signal
	signalTerminatedCode = -1
)

// Health is off by default. DrainDelay creates the observation window by configuration rather than racing for it
const drainConfig = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 5
health:
  enabled: true
  port: {{.HealthPort}}
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

// A custom Yaegi provider holds a request open while shutdown runs
const slowProviderConfig = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 1
  timeout: 30
health:
  enabled: true
  port: {{.HealthPort}}
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
  - id: slow
    provider:
      name: custom
      script: |
        package custom

        import (
            "time"

            "tilegroxy/tilegroxy"
        )

        func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
        ) (tilegroxy.ProviderContext, error) {
            return tilegroxy.ProviderContext{AuthBypass: true}, nil
        }

        func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages) (*tilegroxy.Image, error) {
            time.Sleep(5 * time.Second)
            return &tilegroxy.Image{Content: []byte{0x01, 0x02}}, nil
        }
`

// Lets tests assert ordering against an observed transition rather than a sleep
func waitForDraining(t *testing.T, inst *Instance) {
	t.Helper()

	Until(t, drainObserveTimeout, "health to report draining", func() bool {
		resp, err := http.Get(inst.HealthURL() + "/health")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()

		return resp.StatusCode == http.StatusServiceUnavailable
	})
}

func Test_Shutdown_SigtermExitsZero(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	inst.Signal(syscall.SIGTERM)

	assert.Equal(t, 0, inst.WaitExit(exitTimeout))
}

func Test_Shutdown_SigintExitsZero(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	inst.Signal(syscall.SIGINT)

	assert.Equal(t, 0, inst.WaitExit(exitTimeout))
}

// Readiness must fail before the listener closes or traffic keeps routing here. Tiles must still serve during the drain delay
func Test_Shutdown_ReadinessFailsBeforeListenerCloses(t *testing.T) {
	inst := Start(t, Config{Raw: drainConfig})

	inst.GetHealth().ExpectStatus(http.StatusOK)

	inst.Signal(syscall.SIGTERM)

	waitForDraining(t, inst)

	// The drain delay is still running, so tiles must still serve
	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)

	assert.Equal(t, 0, inst.WaitExit(exitTimeout))
}

// A pod whose liveness fails during drain is SIGKILLed instead of finishing
func Test_Shutdown_LivenessStaysOKWhileDraining(t *testing.T) {
	inst := Start(t, Config{Raw: drainConfig})

	inst.Signal(syscall.SIGTERM)

	waitForDraining(t, inst)

	inst.GetHealth().
		ExpectStatus(http.StatusServiceUnavailable).
		ExpectBodyContains("draining")

	assert.Equal(t, 0, inst.WaitExit(exitTimeout))
}

// A refusal after the listener closes is fine, but a reset accepted connection never is
func Test_Shutdown_UnderLoadDropsNothingMidFlight(t *testing.T) {
	inst := Start(t, Config{Raw: drainConfig})

	load := inst.StartLoad("/tiles/color/8/12/32", loadWorkers)
	time.Sleep(loadWarmup) // Deliberately generating load for a window.

	inst.Signal(syscall.SIGTERM)
	require.Equal(t, 0, inst.WaitExit(exitTimeout))

	res := load.Stop()

	assert.Positive(t, res.Total)
	assert.Equal(t, res.TransportErrors, res.RefusedErrors,
		"every transport error should be a refused connection after the listener closed, not a mid-flight reset")

	for code, n := range res.ByStatus {
		assert.Equal(t, http.StatusOK, code, "unexpected status %v seen %v times during shutdown", code, n)
	}
}

// docker stop then kill. NotifyContext restores the default disposition, so the second SIGTERM terminates (-1) rather than hangs
func Test_Shutdown_SecondSignalDoesNotHangOrPanic(t *testing.T) {
	inst := Start(t, Config{Raw: drainConfig})

	inst.Signal(syscall.SIGTERM)
	waitForDraining(t, inst)
	inst.Signal(syscall.SIGTERM)

	code := inst.WaitExit(exitTimeout)

	assert.NotContains(t, inst.Output(), "panic")
	assert.Contains(t, []int{0, 1, signalTerminatedCode}, code)
}

// DrainDelay 0 is documented for when a preStop hook covers the delay. Asserts ordering, not absolute duration
func Test_Shutdown_ZeroDrainDelayIsFasterThanFive(t *testing.T) {
	fast := Start(t, Config{Raw: `
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
`})

	start := time.Now()
	fast.Signal(syscall.SIGTERM)
	require.Equal(t, 0, fast.WaitExit(exitTimeout))
	fastElapsed := time.Since(start)

	slow := Start(t, Config{Raw: drainConfig})

	start = time.Now()
	slow.Signal(syscall.SIGTERM)
	require.Equal(t, 0, slow.WaitExit(exitTimeout))
	slowElapsed := time.Since(start)

	assert.Less(t, fastElapsed, slowElapsed, "drainDelay 0 should shut down faster than drainDelay 5")
}

// The provider sleeps 5s and the signal lands at 1s, so passing requires finishing the remaining work
func Test_Shutdown_InFlightRequestCompletes(t *testing.T) {
	inst := Start(t, Config{Raw: slowProviderConfig})

	type result struct {
		err    error
		status int
	}

	done := make(chan result, 1)

	go func() {
		client := &http.Client{Timeout: Scale(inFlightClientTimeout)}

		resp, err := client.Get(inst.BaseURL() + "/tiles/slow/8/12/32")
		if err != nil {
			done <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		_, _ = io.Copy(io.Discard, resp.Body)
		done <- result{status: resp.StatusCode}
	}()

	time.Sleep(loadWarmup) // Deliberately letting the request get in flight before signalling.

	inst.Signal(syscall.SIGTERM)

	res := <-done

	require.NoError(t, res.err, "an in-flight request was broken by shutdown")
	assert.Equal(t, http.StatusOK, res.status)

	assert.Equal(t, 0, inst.WaitExit(exitTimeout))
}

// Overrunning the budget gets SIGKILLed by the runtime. The provider sleeps 30s against a 2s budget
func Test_Shutdown_TimeoutIsAHardCeiling(t *testing.T) {
	inst := Start(t, Config{Raw: `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
  timeout: 30
  shutdownTimeout: 2
layers:
  - id: slow
    provider:
      name: custom
      script: |
        package custom

        import (
            "time"

            "tilegroxy/tilegroxy"
        )

        func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
        ) (tilegroxy.ProviderContext, error) {
            return tilegroxy.ProviderContext{AuthBypass: true}, nil
        }

        func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages) (*tilegroxy.Image, error) {
            time.Sleep(30 * time.Second)
            return &tilegroxy.Image{Content: []byte{0x01, 0x02}}, nil
        }
`})

	go func() {
		client := &http.Client{Timeout: Scale(inFlightClientTimeout)}

		resp, err := client.Get(inst.BaseURL() + "/tiles/slow/8/12/32")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	time.Sleep(loadWarmup) // Deliberately letting the request get in flight before signalling.

	start := time.Now()
	inst.Signal(syscall.SIGTERM)
	inst.WaitExit(exitTimeout)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, Scale(shutdownCeiling),
		"shutdown must respect its budget rather than waiting out the 30s request")
}
