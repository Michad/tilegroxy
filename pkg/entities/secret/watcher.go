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
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

// Reasons a secret watch hands to ReloadFunc, recorded on the resulting audit event
const (
	ReasonSecretRotation = "secret_rotation"
	ReasonSecretTTL      = "secret_ttl"
)

// Records every resolved key so a background poll can notice a rotation and rebuild against fresh values
type watchingSecreter struct {
	backend   Secreter
	batchSize int
	reload    func(reason string)

	mu       sync.Mutex
	versions map[string]string
	values   map[string]string
	closed   bool

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func newWatchingSecreter(backend Secreter, _ secretWatchConfig, batchSize int, reload func(reason string)) *watchingSecreter {
	return &watchingSecreter{
		backend:   backend,
		batchSize: batchSize,
		reload:    reload,
		versions:  make(map[string]string),
		values:    make(map[string]string),
		stop:      make(chan struct{}),
	}
}

// start launches the pollers. Separate from construction so tests can drive checkOnce directly
func (w *watchingSecreter) start(cfg secretWatchConfig) {
	w.wg.Add(1)
	go w.runTicker(time.Duration(cfg.WatchInterval)*time.Second, func(ctx context.Context) {
		w.checkOnce(ctx)
	})

	if cfg.TTL > 0 {
		w.wg.Add(1)
		go w.runTicker(time.Duration(cfg.TTL)*time.Second, func(ctx context.Context) {
			slog.InfoContext(ctx, "Reloading configuration because the secret TTL elapsed")
			w.triggerReload(ReasonSecretTTL)
		})
	}
}

func (w *watchingSecreter) runTicker(every time.Duration, tick func(context.Context)) {
	defer w.wg.Done()

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.safeTick(tick)
		}
	}
}

// A panic in a poll must not take down the process or stop future polls
func (w *watchingSecreter) safeTick(tick func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Secret watch poll panicked", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	tick(context.Background())
}

func (w *watchingSecreter) Lookup(ctx context.Context, key string) (string, string, error) {
	w.mu.Lock()
	cached, ok := w.values[key]
	version := w.versions[key]
	w.mu.Unlock()

	if ok {
		return cached, version, nil
	}

	value, version, err := w.backend.Lookup(ctx, key)
	if err != nil {
		return "", "", err
	}

	w.mu.Lock()
	w.values[key] = value
	w.versions[key] = version
	w.mu.Unlock()

	return value, version, nil
}

func (w *watchingSecreter) Check(ctx context.Context, keys []string) ([]string, error) {
	return w.backend.Check(ctx, keys)
}

// checkOnce polls every watchable key and reloads once if any version moved
func (w *watchingSecreter) checkOnce(ctx context.Context) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}

	keys := make([]string, 0, len(w.versions))
	recorded := make(map[string]string, len(w.versions))
	for k, v := range w.versions {
		// An empty recorded version means the backend cannot version this key, so polling it is waste
		if v == "" {
			continue
		}
		keys = append(keys, k)
		recorded[k] = v
	}
	w.mu.Unlock()

	if len(keys) == 0 {
		return
	}

	changed := false

	for start := 0; start < len(keys); start += w.batchSize {
		end := min(start+w.batchSize, len(keys))
		batch := keys[start:end]

		versions, err := w.backend.Check(ctx, batch)
		if err != nil {
			slog.WarnContext(ctx, "Failed to check secrets for changes, keeping last known values: "+err.Error())
			continue
		}

		if len(versions) != len(batch) {
			slog.WarnContext(ctx, "Secret change check returned a mismatched number of versions, ignoring")
			continue
		}

		for i, key := range batch {
			// An empty version now means the backend stopped being able to tell, not that it changed
			if versions[i] == "" || versions[i] == recorded[key] {
				continue
			}
			changed = true
		}
	}

	if changed {
		slog.InfoContext(ctx, "Reloading configuration because a watched secret changed")
		w.triggerReload(ReasonSecretRotation)
	}
}

func (w *watchingSecreter) triggerReload(reason string) {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()

	if closed || w.reload == nil {
		return
	}

	w.reload(reason)
}

// Close stops the pollers and releases the wrapped backend
func (w *watchingSecreter) Close(ctx context.Context) error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()

	w.stopOnce.Do(func() { close(w.stop) })
	w.wg.Wait()

	return lifecycle.CloseIfCloser(ctx, w.backend)
}
