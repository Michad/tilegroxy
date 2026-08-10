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

package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

const reloadTestSignal = syscall.SIGUSR2

func init() {
	InterruptFlags = append(InterruptFlags, reloadTestSignal)
}

// freePort asks the kernel for an unused TCP port and immediately releases it. The gap before the
// server binds is racy, but less so than hardcoded ports colliding with anything else in CI.
func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())

	return port
}

func dialPort(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	conn.Close()

	return true
}

func waitForPort(t *testing.T, port int) {
	t.Helper()

	require.Eventually(t, func() bool { return dialPort(port) }, 10*time.Second, 50*time.Millisecond,
		"timed out waiting for port %v to accept connections", port)
}

// tryHealthStatus fetches the health endpoint's status, reporting failure to reach or parse it
// rather than asserting, since in polling loops that's an expected intermediate state.
func tryHealthStatus(port int) (string, bool) {
	resp, err := http.DefaultClient.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/health")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	jsonByte, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	jsonMap := make(map[string]any)
	if json.Unmarshal(jsonByte, &jsonMap) != nil {
		return "", false
	}

	status, ok := jsonMap["status"].(string)
	return status, ok
}

func waitForHealthStatus(t *testing.T, port int, want string) {
	t.Helper()

	var last string
	require.Eventually(t, func() bool {
		status, ok := tryHealthStatus(port)
		if ok {
			last = status
		}
		return ok && status == want
	}, 15*time.Second, 100*time.Millisecond, "health status never became %v (last seen %q)", want, last)
}

// healthTestConfig builds a minimal config with health enabled on a dynamically chosen port and a
// single working "static" layer that the tile health check can exercise.
func healthTestConfig(t *testing.T) (config.Config, int) {
	t.Helper()

	healthPort := freePort(t)

	cfg := config.DefaultConfig()
	cfg.Server.Port = freePort(t)
	cfg.Server.Health.Enabled = true
	cfg.Server.Health.Port = healthPort
	cfg.Server.Health.Host = "127.0.0.1"
	cfg.Server.Health.Checks = []map[string]any{
		{"name": "tile", "layer": "test", "delay": 1},
	}
	cfg.Layers = []config.LayerConfig{
		{ID: "test", Provider: map[string]any{"name": "static", "color": "FFFFFF"}},
	}

	return cfg, healthPort
}

// entitiesFor wraps a LayerGroup in the entities bundle ListenAndServe takes. The health
// subsystem reads nothing else, and Entities.Close tolerates the rest being nil.
func entitiesFor(lg *layer.LayerGroup) *entities.Entities {
	return &entities.Entities{LayerGroup: lg}
}

// startServer boots ListenAndServe in the background, waits until its health endpoint is live, and
// returns the reload callback it published, registering cleanup that stops the server.
//
// ListenAndServe writes through reloadPtr on its own goroutine with no synchronization, so the
// value is handed over through a channel from the onReloadPtrSet hook rather than read directly.
func startServer(t *testing.T, cfg *config.Config, lg *layer.LayerGroup) reloadEntitiesFunc {
	t.Helper()

	var reloadFn reloadEntitiesFunc

	published := make(chan reloadEntitiesFunc, 1)

	onReloadPtrSet = func() { published <- reloadFn }
	t.Cleanup(func() { onReloadPtrSet = nil })

	done := make(chan error, 1)
	go func() {
		err := ListenAndServe(cfg, entitiesFor(lg), &reloadFn)
		// Unblock the handoff if the server died before ever publishing.
		select {
		case published <- nil:
		default:
		}
		done <- err
	}()

	t.Cleanup(func() {
		_ = syscall.Kill(syscall.Getpid(), reloadTestSignal)
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("ListenAndServe did not return after interrupt")
		}
	})

	select {
	case fn := <-published:
		require.NotNil(t, fn, "server exited before publishing a reload callback")
		return fn
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the server to publish its reload callback")
		return nil
	}
}

// Health check tickers close over the LayerGroup they were built against, so without a rebuild on
// reload a layer that a reload fixes or breaks stays invisible to health checks. Driven through
// ListenAndServe's own reloadPtr, keeping the layer ID and check config identical throughout.
func Test_ListenAndServe_HealthChecksRebuildOnReload(t *testing.T) {
	cfg1, healthPort := healthTestConfig(t)

	lg1, err := layer.ConstructLayerGroup(cfg1, nil, nil, nil)
	require.NoError(t, err)

	reloadFn := startServer(t, &cfg1, lg1)

	waitForPort(t, healthPort)
	waitForHealthStatus(t, healthPort, "ok")

	cfg2 := cfg1
	cfg2.Layers = []config.LayerConfig{
		{ID: "test", Provider: map[string]any{"name": "fail", "onauth": true, "message": "simulated failure"}},
	}

	lg2, err := layer.ConstructLayerGroup(cfg2, nil, nil, nil)
	require.NoError(t, err)

	require.NoError(t, reloadFn(&cfg2, entitiesFor(lg2)))

	waitForHealthStatus(t, healthPort, "error")
}

// pkg/config dispatches each config-change event on its own goroutine, so concurrent reloads are
// reachable in production. Two reloads that both invoke the same generation's shutdown func
// deadlock the second caller, so the body runs behind a timeout to fail rather than hang.
func Test_ListenAndServe_ConcurrentHealthReloadsDoNotDeadlock(t *testing.T) {
	cfg, healthPort := healthTestConfig(t)

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	reloadFn := startServer(t, &cfg, lg)

	waitForPort(t, healthPort)
	waitForHealthStatus(t, healthPort, "ok")

	const reloadCount = 4

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		var wg sync.WaitGroup
		for range reloadCount {
			wg.Add(1)
			go func() {
				defer wg.Done()

				lgN, errN := layer.ConstructLayerGroup(cfg, nil, nil, nil)
				if errN != nil {
					t.Error(errN)
					return
				}
				if errN = reloadFn(&cfg, entitiesFor(lgN)); errN != nil {
					t.Error(errN)
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatalf("%v concurrent reloads deadlocked - they did not all complete within the timeout", reloadCount)
	}

	// Exactly one generation should own the health port and be serving normally.
	waitForHealthStatus(t, healthPort, "ok")
}

// A rebuild failing on bad check config leaves the old generation already shut down, and the tile
// handler reload has succeeded by that point, so the process keeps serving tiles with its liveness
// endpoint down. The failure has to be reported, no stale shutdown pointer retained, and a later
// good reload has to bring health back.
func Test_ListenAndServe_FailedHealthRebuildRecovers(t *testing.T) {
	cfg, healthPort := healthTestConfig(t)

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	reloadFn := startServer(t, &cfg, lg)

	waitForPort(t, healthPort)
	waitForHealthStatus(t, healthPort, "ok")

	// Reload with a check name that doesn't resolve to any registered health check.
	badCfg := cfg
	badCfg.Server.Health.Checks = []map[string]any{
		{"name": "this-check-does-not-exist", "delay": 1},
	}

	badLg, err := layer.ConstructLayerGroup(badCfg, nil, nil, nil)
	require.NoError(t, err)

	require.Error(t, reloadFn(&badCfg, entitiesFor(badLg)), "a reload with an unknown health check name must surface an error")

	// A subsequent good reload must bring the health endpoint back.
	goodLg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	require.NoError(t, reloadFn(&cfg, entitiesFor(goodLg)))

	waitForPort(t, healthPort)
	waitForHealthStatus(t, healthPort, "ok")
}

// After a failed rebuild the shutdown pointer must not still reference the previous generation's
// already-invoked shutdown func, which ListenAndServe's final shutdown would call a second time.
func Test_healthReloader_FailedRebuildDoesNotRetainStalePointer(t *testing.T) {
	cfg, _ := healthTestConfig(t)

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	ctx := context.Background()

	var healthMutex sync.Mutex

	oldCalls := 0
	oldShutdown := func(context.Context) error {
		oldCalls++
		return nil
	}
	healthShutdown := oldShutdown

	badCfg := cfg
	badCfg.Server.Health.Checks = []map[string]any{
		{"name": "this-check-does-not-exist", "delay": 1},
	}

	require.Error(t, healthReloader(ctx, &badCfg, entitiesFor(lg), &healthMutex, &healthShutdown))
	require.Equal(t, 1, oldCalls, "the previous generation should have been shut down exactly once")

	if healthShutdown != nil {
		// Whatever remains must be a new func tearing down the partial generation.
		require.NoError(t, healthShutdown(ctx))
		require.Equal(t, 1, oldCalls, "the retained shutdown func must not be the stale previous generation's")
	}
}
