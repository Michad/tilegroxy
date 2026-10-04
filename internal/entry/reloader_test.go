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

package entry

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/internal/secrets"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readyReloader(cfg *config.Config, swap swapFunc) *Reloader {
	r := NewReloader()
	r.start(cfg)
	r.ready(swap)

	return r
}

func portConfig(port int) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Server.Port = port

	return &cfg
}

// Reports the port of every config it's handed
func recordingSwap() (swapFunc, chan int) {
	seen := make(chan int, 64)

	return func(c *config.Config, _ *entities.Entities) error {
		seen <- c.Server.Port
		return nil
	}, seen
}

func expectPort(t *testing.T, seen chan int, want int) {
	t.Helper()

	select {
	case got := <-seen:
		assert.Equal(t, want, got)
	case <-time.After(5 * time.Second):
		t.Fatalf("reload to port %v never applied", want)
	}
}

func expectNoMore(t *testing.T, seen chan int) {
	t.Helper()

	select {
	case got := <-seen:
		t.Fatalf("unexpected extra reload to port %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// Waits out anything scheduled so assertions don't race a background reload
func settle(t *testing.T, r *Reloader) {
	t.Helper()

	require.Eventually(t, func() bool {
		r.run.Lock()
		defer r.run.Unlock()

		r.mu.Lock()
		defer r.mu.Unlock()

		return !r.scheduled && r.pending == nil
	}, 5*time.Second, 5*time.Millisecond)
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func Test_Reloader_AppliesLatestReloadQueuedBeforeReady(t *testing.T) {
	r := NewReloader()
	r.start(portConfig(1))

	require.NoError(t, r.Reload(portConfig(2)))
	require.NoError(t, r.Reload(portConfig(3)))

	swap, seen := recordingSwap()
	r.ready(swap)

	expectPort(t, seen, 3)
	expectNoMore(t, seen)
	settle(t, r)
}

func Test_Reloader_SecretRequestBeforeReadyAppliesOnReady(t *testing.T) {
	var buf lockedBuffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	r := NewReloader()
	r.start(portConfig(7))

	r.requestLive(secrets.ReasonSecretTTL)

	swap, seen := recordingSwap()
	r.ready(swap)

	expectPort(t, seen, 7)
	settle(t, r)
	assert.Contains(t, buf.String(), secrets.ReasonSecretTTL)
}

// A file change queued during startup is newer than the live config a secret would re-resolve
func Test_Reloader_SecretRequestKeepsQueuedFileReload(t *testing.T) {
	r := NewReloader()
	r.start(portConfig(1))

	require.NoError(t, r.Reload(portConfig(2)))
	r.requestLive(secrets.ReasonSecretRotation)

	swap, seen := recordingSwap()
	r.ready(swap)

	expectPort(t, seen, 2)
	expectNoMore(t, seen)
	settle(t, r)
}

func Test_Reloader_QueuedReloadFailureIsAudited(t *testing.T) {
	var buf lockedBuffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	bad := config.DefaultConfig()
	bad.Error.Mode = "not-a-real-mode"

	r := NewReloader()
	r.start(portConfig(1))

	require.NoError(t, r.Reload(&bad))

	swap, seen := recordingSwap()
	r.ready(swap)

	settle(t, r)
	expectNoMore(t, seen)
	assert.Contains(t, buf.String(), audit.OutcomeFailure)
}

// Whichever runs first, the fresh reload must win over the queued one
func Test_Reloader_NewerReloadWinsOverQueued(t *testing.T) {
	for range 20 {
		r := NewReloader()
		r.start(portConfig(1))

		require.NoError(t, r.Reload(portConfig(2)))

		var mu sync.Mutex
		var last int

		r.ready(func(c *config.Config, _ *entities.Entities) error {
			mu.Lock()
			last = c.Server.Port
			mu.Unlock()

			return nil
		})

		require.NoError(t, r.Reload(portConfig(3)))
		settle(t, r)

		mu.Lock()
		assert.Equal(t, 3, last)
		mu.Unlock()
	}
}

func Test_Reloader_NeverOverlaps(t *testing.T) {
	var mu sync.Mutex
	running, maxRunning, calls := 0, 0, 0

	r := readyReloader(portConfig(1), func(_ *config.Config, _ *entities.Entities) error {
		mu.Lock()
		running++
		calls++
		maxRunning = max(maxRunning, running)
		mu.Unlock()

		time.Sleep(5 * time.Millisecond)

		mu.Lock()
		running--
		mu.Unlock()

		return nil
	})

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			if i%2 == 0 {
				r.requestLive(secrets.ReasonSecretRotation)
			} else {
				assert.NoError(t, r.apply(portConfig(i), audit.ReasonSignal))
			}
		})
	}
	wg.Wait()
	settle(t, r)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, 1, maxRunning)
	assert.GreaterOrEqual(t, calls, 8)
}

// Closing a generation waits on its secret pollers, so a poller blocked on a reload would deadlock
func Test_Reloader_SecretRequestDoesNotBlockDuringReload(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	var mu sync.Mutex
	calls := 0

	r := readyReloader(portConfig(1), func(_ *config.Config, _ *entities.Entities) error {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()

		if first {
			close(entered)
			<-release
		}

		return nil
	})

	go func() { assert.NoError(t, r.Reload(portConfig(2))) }()

	<-entered

	returned := make(chan struct{})
	go func() {
		for range 5 {
			r.requestLive(secrets.ReasonSecretRotation)
		}
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("a secret reload request blocked behind a running reload")
	}

	close(release)
	settle(t, r)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, 2, calls, "requests made during one reload collapse into a single follow-up")
}

func Test_Reloader_IgnoresReloadsAfterStop(t *testing.T) {
	r := NewReloader()
	r.start(portConfig(1))
	r.stop()

	require.NoError(t, r.Reload(portConfig(2)))
	r.requestLive(secrets.ReasonSecretRotation)

	swap, seen := recordingSwap()
	r.ready(swap)

	require.NoError(t, r.Reload(portConfig(3)))
	expectNoMore(t, seen)
}

func Test_Reloader_StopDropsScheduledReload(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)

	r := readyReloader(portConfig(1), func(_ *config.Config, _ *entities.Entities) error {
		entered <- struct{}{}
		<-release

		return nil
	})

	done := make(chan struct{})
	go func() {
		assert.NoError(t, r.Reload(portConfig(2)))
		close(done)
	}()

	<-entered
	r.requestLive(secrets.ReasonSecretRotation)
	r.stop()
	close(release)
	<-done

	settle(t, r)
	assert.Empty(t, entered)
}
