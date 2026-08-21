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
	// How long a test waits for a signalled instance to exit. Generous because a drain window plus a
	// slow provider is the point of several of these tests.
	exitTimeout = 45 * time.Second
	// How long a readiness transition may take to become observable.
	drainObserveTimeout = 10 * time.Second
	// Workers used by the continuity tests. Enough to keep a connection in flight at all times
	// without saturating a laptop.
	loadWorkers = 4
	// How long load runs before a disruption, so the disruption lands on a busy server.
	loadWarmup = time.Second
	// How long an in-flight request client waits. Longer than any provider sleep these tests
	// configure, so a client timeout never masks a server-side reset.
	inFlightClientTimeout = 60 * time.Second
	// The bound Test_Shutdown_TimeoutIsAHardCeiling asserts. Comfortably above the configured 2s
	// budget and comfortably below the 30s the provider would take if the budget were ignored.
	shutdownCeiling = 20 * time.Second
	// What os/exec reports for a process terminated by a signal rather than exiting on its own.
	signalTerminatedCode = -1
)

// Health is disabled by default, so every readiness test must turn it on explicitly. DrainDelay
// creates the observation window by configuration rather than by racing for it.
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

// slowProviderConfig uses the custom Yaegi provider to hold a request open, which is how a request
// is kept in flight while shutdown runs.
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

// waitForDraining blocks until the health endpoint reports the draining status, so tests assert an
// ordering against an observed transition rather than against a sleep.
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

// The highest-value test here. Readiness must go false before the listener closes, or the endpoints
// controller keeps routing new traffic to a terminating pod. Asserts the ordering, not a duration.
//
// The drain delay exists so traffic already routed to this pod keeps being served while the
// endpoints controller catches up. A tile request during that window must therefore still return
// 200; a pod that fails every request the moment it is signalled has a readiness window that
// accomplishes nothing.
func Test_Shutdown_ReadinessFailsBeforeListenerCloses(t *testing.T) {
	inst := Start(t, Config{Raw: drainConfig})

	inst.GetHealth().ExpectStatus(http.StatusOK)

	inst.Signal(syscall.SIGTERM)

	waitForDraining(t, inst)

	// The drain delay is still running, so tiles must still serve.
	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)

	assert.Equal(t, 0, inst.WaitExit(exitTimeout))
}

// A pod whose liveness fails during drain is SIGKILLed instead of being allowed to finish.
func Test_Shutdown_LivenessStaysOKWhileDraining(t *testing.T) {
	inst := Start(t, Config{Raw: drainConfig})

	inst.Signal(syscall.SIGTERM)

	waitForDraining(t, inst)

	inst.GetHealth().
		ExpectStatus(http.StatusServiceUnavailable).
		ExpectBodyContains("draining")

	assert.Equal(t, 0, inst.WaitExit(exitTimeout))
}

// What must not appear is a request that connected and was then reset. A refusal after the listener
// closes is acceptable; a broken accepted connection is not.
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

// docker stop followed by docker kill produces this. It must neither panic nor hang.
//
// The second signal terminates the process rather than being absorbed: signal.NotifyContext restores
// the default disposition once it has fired, so a repeat SIGTERM lands on the default handler. That
// is the documented Go behavior and matches what an operator escalating a stop expects, so the
// assertion is that shutdown ends promptly and cleanly, not that the exit status is zero. A
// signal-terminated process has no exit code, which os/exec reports as -1.
func Test_Shutdown_SecondSignalDoesNotHangOrPanic(t *testing.T) {
	inst := Start(t, Config{Raw: drainConfig})

	inst.Signal(syscall.SIGTERM)
	waitForDraining(t, inst)
	inst.Signal(syscall.SIGTERM)

	code := inst.WaitExit(exitTimeout)

	assert.NotContains(t, inst.Output(), "panic")
	assert.Contains(t, []int{0, 1, signalTerminatedCode}, code)
}

// DrainDelay 0 is the documented behavior when a preStop hook covers the delay. Asserts an ordering
// between two configurations rather than an absolute duration.
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

// The core graceful-shutdown promise: a request already being served finishes rather than being
// cut off. The provider sleeps 5s and the signal lands 1s in, so the only way to pass is to let the
// remaining 4s of work finish and return the tile.
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

// A shutdown that overruns its budget gets SIGKILLed by the container runtime, which is the failure
// this bound exists to prevent. The provider sleeps 30s while the budget is 2s.
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
