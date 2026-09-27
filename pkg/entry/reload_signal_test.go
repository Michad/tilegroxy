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

//go:build !windows

package tg

import (
	"bytes"
	"errors"
	"log/slog"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readyNow() chan struct{} {
	ready := make(chan struct{})
	close(ready)

	return ready
}

func captureAudit(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	return &buf
}

func Test_ReloadSignal_ReloadsFromSource(t *testing.T) {
	want := config.DefaultConfig()
	want.Server.Port = 4321

	reasons := make(chan string, 1)
	var got *config.Config

	stop := watchReloadSignal(
		readyNow(),
		func() (config.Config, error) { return want, nil },
		func(c *config.Config, reason string) error {
			got = c
			reasons <- reason
			return nil
		},
		nil,
	)
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))

	select {
	case reason := <-reasons:
		assert.Equal(t, audit.ReasonSignal, reason)
		assert.Equal(t, 4321, got.Server.Port)
	case <-time.After(5 * time.Second):
		t.Fatal("SIGHUP did not trigger a reload")
	}
}

// Signals landing while a reload runs must neither overlap it nor queue one reload per signal
func Test_ReloadSignal_SerializesAndCoalesces(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	var mu sync.Mutex
	running, maxRunning, calls := 0, 0, 0

	stop := watchReloadSignal(
		readyNow(),
		func() (config.Config, error) { return config.DefaultConfig(), nil },
		func(_ *config.Config, _ string) error {
			mu.Lock()
			running++
			calls++
			maxRunning = max(maxRunning, running)
			first := calls == 1
			mu.Unlock()

			if first {
				close(started)
				<-release
			}

			mu.Lock()
			running--
			mu.Unlock()

			return nil
		},
		nil,
	)
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))
	<-started

	for range 5 {
		require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))
	}

	// Give the signals time to be delivered before the first reload finishes
	time.Sleep(200 * time.Millisecond)
	close(release)

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 2 && running == 0
	}, 5*time.Second, 10*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, calls, "signals received during a reload should collapse into one follow-up")
	assert.Equal(t, 1, maxRunning, "reloads must not overlap")
}

func Test_ReloadSignal_LoadErrorIsAuditedAndSkipsReload(t *testing.T) {
	buf := captureAudit(t)

	reloadCalled := false

	reloadFromSource(
		func() (config.Config, error) { return config.Config{}, errors.New("file vanished") },
		func(_ *config.Config, _ string) error {
			reloadCalled = true
			return nil
		},
		nil,
	)

	assert.False(t, reloadCalled)
	assert.Contains(t, buf.String(), audit.OutcomeFailure)
	assert.Contains(t, buf.String(), audit.ReasonSignal)
	assert.Contains(t, buf.String(), "file vanished")
}

func Test_ReloadSignal_RecoversLoadPanic(t *testing.T) {
	buf := captureAudit(t)

	assert.NotPanics(t, func() {
		reloadFromSource(
			func() (config.Config, error) { panic("bad loader") },
			func(_ *config.Config, _ string) error { return nil },
			nil,
		)
	})

	assert.Contains(t, buf.String(), "bad loader")
}

func Test_ReloadSignal_ReloadErrorIsReported(t *testing.T) {
	var out bytes.Buffer

	reloadFromSource(
		func() (config.Config, error) { return config.DefaultConfig(), nil },
		func(_ *config.Config, _ string) error { return errors.New("swap rejected") },
		&out,
	)

	assert.Equal(t, "Error: swap rejected\n", out.String())
}

// A signal sent while the server is still starting must be held until it can apply, not dropped
func Test_ReloadSignal_WaitsForReady(t *testing.T) {
	ready := make(chan struct{})
	reloads := make(chan struct{}, 1)

	stop := watchReloadSignal(
		ready,
		func() (config.Config, error) { return config.DefaultConfig(), nil },
		func(_ *config.Config, _ string) error {
			reloads <- struct{}{}
			return nil
		},
		nil,
	)
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))

	select {
	case <-reloads:
		t.Fatal("reload ran before the server was ready")
	case <-time.After(200 * time.Millisecond):
	}

	close(ready)

	select {
	case <-reloads:
	case <-time.After(5 * time.Second):
		t.Fatal("signal received before ready was dropped")
	}
}

// Stopping must not leave the goroutine blocked waiting for a server that never became ready
func Test_ReloadSignal_StopWhileWaitingForReady(t *testing.T) {
	stop := watchReloadSignal(
		make(chan struct{}),
		func() (config.Config, error) { return config.DefaultConfig(), nil },
		func(_ *config.Config, _ string) error { return nil },
		nil,
	)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))
	time.Sleep(100 * time.Millisecond)

	assert.NotPanics(t, stop)
}
