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

package secret

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedSecreter lets a test drive versions and count backend calls
type scriptedSecreter struct {
	mu           sync.Mutex
	version      string
	lookupCalls  int
	checkCalls   int
	checkBatches [][]string
	checkErr     error
	closed       bool
}

func (s *scriptedSecreter) Lookup(_ context.Context, key string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookupCalls++
	return "value-" + key, s.version, nil
}

func (s *scriptedSecreter) Check(_ context.Context, keys []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkCalls++
	s.checkBatches = append(s.checkBatches, append([]string(nil), keys...))

	if s.checkErr != nil {
		return nil, s.checkErr
	}

	out := make([]string, len(keys))
	for i := range keys {
		out[i] = s.version
	}
	return out, nil
}

func (s *scriptedSecreter) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *scriptedSecreter) setVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version = v
}

func (s *scriptedSecreter) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupCalls, s.checkCalls
}

// newTestWatcher builds a wrapper with its tickers disabled so tests drive checkOnce directly
func newTestWatcher(t *testing.T, backend Secreter, batchSize int, reload func(reason string)) *watchingSecreter {
	t.Helper()
	return newWatchingSecreter(backend, secretWatchConfig{Watch: true, WatchInterval: defaultWatchInterval}, batchSize, reload)
}

func Test_Watcher_LookupRecordsKeyAndServesFromCache(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	w := newTestWatcher(t, backend, 10, func(_ string) {})

	v, ver, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "value-a", v)
	assert.Equal(t, "v1", ver)

	v2, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "value-a", v2)

	lookups, _ := backend.counts()
	assert.Equal(t, 1, lookups, "second lookup must be served from cache")
}

func Test_Watcher_ChangedVersionTriggersExactlyOneReload(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	var reloads int
	w := newTestWatcher(t, backend, 10, func(_ string) { reloads++ })

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)
	_, _, err = w.Lookup(context.Background(), "b")
	require.NoError(t, err)

	backend.setVersion("v2")
	w.checkOnce(context.Background())

	assert.Equal(t, 1, reloads, "one reload for the whole generation, not one per key")
}

func Test_Watcher_UnchangedVersionTriggersNoReload(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	var reloads int
	w := newTestWatcher(t, backend, 10, func(_ string) { reloads++ })

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)

	w.checkOnce(context.Background())

	assert.Equal(t, 0, reloads)
}

func Test_Watcher_ChunksKeysToBatchSize(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	w := newTestWatcher(t, backend, 20, func(_ string) {})

	for i := range 45 {
		_, _, err := w.Lookup(context.Background(), string(rune('a'+i%26))+string(rune('0'+i/26)))
		require.NoError(t, err)
	}

	w.checkOnce(context.Background())

	_, checks := backend.counts()
	assert.Equal(t, 3, checks, "45 keys at batch size 20 is three calls")
}

func Test_Watcher_BatchSizeOneCallsCheckPerKey(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	w := newTestWatcher(t, backend, 1, func(_ string) {})

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)
	_, _, err = w.Lookup(context.Background(), "b")
	require.NoError(t, err)

	w.checkOnce(context.Background())

	_, checks := backend.counts()
	assert.Equal(t, 2, checks)
}

// A key the backend cannot version is dead weight on every poll, so it is never sent to Check
func Test_Watcher_EmptyRecordedVersionIsNeverChecked(t *testing.T) {
	backend := &scriptedSecreter{version: ""}
	var reloads int
	w := newTestWatcher(t, backend, 10, func(_ string) { reloads++ })

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)

	w.checkOnce(context.Background())

	_, checks := backend.counts()
	assert.Equal(t, 0, checks)
	assert.Equal(t, 0, reloads)
}

func Test_Watcher_CheckErrorLeavesVersionsIntactAndSkipsReload(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	var reloads int
	w := newTestWatcher(t, backend, 10, func(_ string) { reloads++ })

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)

	backend.mu.Lock()
	backend.checkErr = errors.New("backend down")
	backend.mu.Unlock()

	w.checkOnce(context.Background())
	assert.Equal(t, 0, reloads)

	backend.mu.Lock()
	backend.checkErr = nil
	backend.version = "v2"
	backend.mu.Unlock()

	w.checkOnce(context.Background())
	assert.Equal(t, 1, reloads, "the recorded version survived the failure, so the change is still caught")
}

// A backend degrading into "cannot tell" must not reload forever
func Test_Watcher_VersionGoingEmptyTriggersNoReload(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	var reloads int
	w := newTestWatcher(t, backend, 10, func(_ string) { reloads++ })

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)

	backend.setVersion("")
	w.checkOnce(context.Background())

	assert.Equal(t, 0, reloads)
}

func Test_Watcher_CloseClosesBackendAndStopsReloads(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	var reloads int
	w := newTestWatcher(t, backend, 10, func(_ string) { reloads++ })

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)

	require.NoError(t, w.Close(context.Background()))

	backend.mu.Lock()
	assert.True(t, backend.closed)
	backend.mu.Unlock()

	backend.setVersion("v2")
	w.checkOnce(context.Background())
	assert.Equal(t, 0, reloads, "a closed wrapper must not reload")
}

func Test_Watcher_CheckIsForwardedToBackendVerbatim(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	w := newTestWatcher(t, backend, 10, func(_ string) {})

	versions, err := w.Check(context.Background(), []string{"x", "y"})
	require.NoError(t, err)
	assert.Equal(t, []string{"v1", "v1"}, versions)
}

// The TTL ticker is the safety net for a backend whose versions never move, so it must reload
// without any version having changed
func Test_Watcher_TTLTickerReloadsWithoutVersionChange(t *testing.T) {
	backend := &scriptedSecreter{version: "v1"}
	var mu sync.Mutex
	reloads := 0

	var lastReason string

	w := newWatchingSecreter(backend, secretWatchConfig{Watch: true, WatchInterval: 3600, TTL: 1}, 10, func(reason string) {
		mu.Lock()
		defer mu.Unlock()
		lastReason = reason
		reloads++
	})
	w.start(secretWatchConfig{Watch: true, WatchInterval: 3600, TTL: 1})
	t.Cleanup(func() { _ = w.Close(context.Background()) })

	_, _, err := w.Lookup(context.Background(), "a")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reloads >= 1
	}, 5*time.Second, 50*time.Millisecond, "the TTL ticker must fire a reload on its own")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, ReasonSecretTTL, lastReason)
}

func Test_ConstructSecreter_WrapsWhenWatching(t *testing.T) {
	s, err := ConstructSecreter(map[string]interface{}{
		"name":          "stub-secreter",
		"value":         "hunter2",
		"watch":         true,
		"watchinterval": 3600,
	}, SecreterDeps{ErrorMessages: testErrorMessages(), ReloadFunc: func(_ string) {}})
	require.NoError(t, err)

	w, ok := s.(*watchingSecreter)
	require.True(t, ok, "watching config must produce a wrapper")
	require.NoError(t, w.Close(context.Background()))
}

func Test_ConstructSecreter_DoesNotWrapWhenNotWatching(t *testing.T) {
	s, err := ConstructSecreter(map[string]interface{}{
		"name":  "stub-secreter",
		"value": "hunter2",
	}, SecreterDeps{ErrorMessages: testErrorMessages(), ReloadFunc: func(_ string) {}})
	require.NoError(t, err)

	_, ok := s.(*watchingSecreter)
	assert.False(t, ok)
}
