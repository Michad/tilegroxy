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
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type swapFunc = func(*config.Config, *entities.Entities) error

// Applies hot reloads one at a time, holding any that arrive before the server is ready
type Reloader struct {
	// Held for a whole reload so reloads apply in order
	run sync.Mutex

	mu        sync.Mutex
	swap      swapFunc
	current   *config.Config
	pending   *pendingReload
	scheduled bool
	stopped   bool
}

type pendingReload struct {
	// Nil re-resolves whichever config is live when the reload runs
	cfg    *config.Config
	reason string
}

// Pass to a single call of Serve
func NewReloader() *Reloader {
	return &Reloader{}
}

// Applies cfg as a file change. Before the server is ready only the latest cfg is kept
func (r *Reloader) Reload(cfg *config.Config) error {
	return r.apply(cfg, audit.ReasonConfigFile)
}

func (r *Reloader) start(cfg *config.Config) {
	r.mu.Lock()
	r.current = cfg
	r.mu.Unlock()
}

func (r *Reloader) apply(cfg *config.Config, reason string) error {
	r.run.Lock()
	defer r.run.Unlock()

	r.mu.Lock()
	swap := r.swap
	stopped := r.stopped

	if swap == nil && !stopped {
		r.pending = &pendingReload{cfg: cfg, reason: reason}
	} else {
		// Built from a newer config than anything still queued
		r.pending = nil
	}
	r.mu.Unlock()

	if stopped {
		slog.Warn("Ignoring configuration reload because the server has stopped", "reason", reason)
		return nil
	}

	if swap == nil {
		slog.Info("Configuration reload will be applied once the server is ready", "reason", reason)
		return nil
	}

	return r.build(swap, cfg, reason)
}

// Must not block, since closing a generation waits for the secret poller calling this
func (r *Reloader) requestLive(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopped {
		return
	}

	if r.pending == nil {
		r.pending = &pendingReload{reason: reason}
	}

	if r.swap == nil {
		slog.Info("Configuration reload will be applied once the server is ready", "reason", reason)
		return
	}

	r.schedule()
}

// Caller must hold mu
func (r *Reloader) schedule() {
	if r.scheduled {
		return
	}

	r.scheduled = true

	go r.runPending()
}

// Hands over the server's swap function and applies anything queued during startup
func (r *Reloader) ready(swap swapFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopped {
		return
	}

	r.swap = swap

	if r.pending != nil {
		r.schedule()
	}
}

func (r *Reloader) stop() {
	r.mu.Lock()
	r.stopped = true
	r.swap = nil
	r.pending = nil
	r.mu.Unlock()
}

func (r *Reloader) runPending() {
	r.run.Lock()
	defer r.run.Unlock()

	r.mu.Lock()
	r.scheduled = false
	next := r.pending
	r.pending = nil
	swap := r.swap
	cfg := r.current
	r.mu.Unlock()

	if next == nil || swap == nil {
		return
	}

	if next.cfg != nil {
		cfg = next.cfg
	}

	if err := r.build(swap, cfg, next.reason); err != nil {
		slog.Error(fmt.Sprintf("Failed to apply a queued configuration reload (%v): %v", next.reason, err))
	}
}

// Caller must hold run
func (r *Reloader) build(swap swapFunc, newCfg *config.Config, reason string) (err error) {
	auditCtx := pkg.BackgroundContext()

	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("config reload panic: %v\n%s", rec, debug.Stack())
			audit.ConfigReload(auditCtx, reason, err)
		}
	}()

	ent, err := configToEntities(auditCtx, *newCfg, r.requestLive)
	if err != nil {
		audit.ConfigReload(auditCtx, reason, err)
		return err
	}

	if err := swap(newCfg, ent); err != nil {
		audit.ConfigReload(auditCtx, reason, err)

		closeCtx, cancel := context.WithTimeout(context.Background(), time.Duration(configload.EffectiveShutdownTimeout(newCfg.Server))*time.Second) // #nosec G115 -- operator-supplied timeout in seconds, far below int64 overflow range
		defer cancel()

		if closeErr := ent.Close(closeCtx); closeErr != nil {
			slog.WarnContext(closeCtx, fmt.Sprintf("Error releasing entities from a failed reload: %v", closeErr))
		}

		return err
	}

	r.mu.Lock()
	r.current = newCfg
	r.mu.Unlock()

	audit.ConfigReload(auditCtx, reason, nil)

	return nil
}
