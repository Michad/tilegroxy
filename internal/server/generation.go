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

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	internalanalytics "github.com/Michad/tilegroxy/internal/analytics"
	"github.com/Michad/tilegroxy/internal/static"

	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
)

// How long before trusting the refcount. Only covers the gap between the locked pointer read and the increment
const generationCloseFloor = 2 * time.Second

// What one request is served from. A reload swaps the pointer and the outgoing generation releases after its last request
type generation struct {
	// Non-reloadable, so startup config carries across reloads. By value so handlers can't reach the live *config.Config
	serverCfg config.ServerConfig
	// Also non-reloadable
	errCfg config.ErrorConfig

	// Sole owner. Requests hold the generation, so a reload can't release entities under one in flight
	all *entities.Entities

	mu       sync.Mutex
	refs     int
	closing  bool
	closed   bool
	closes   int
	closeCtx context.Context //nolint:containedctx // carries the shutdown deadline to a close that happens on whichever goroutine drops the last reference

	// Lets the registry drop its reference after close so superseded generations don't accumulate. Nil until registry.add
	onClosed func()

	// Closed once all.Close returns so a racing second caller waits for the drain instead of treating "closed" as finished
	done chan struct{}
}

func newGeneration(cfg *config.Config, ent *entities.Entities) *generation {
	g := &generation{all: ent, done: make(chan struct{})}

	if cfg != nil {
		g.serverCfg = cfg.Server
		g.errCfg = cfg.Error
	}

	return g
}

func (g *generation) succeededBy(ent *entities.Entities) *generation {
	next := newGeneration(nil, ent)
	next.serverCfg = g.serverCfg
	next.errCfg = g.errCfg

	return next
}

func (g *generation) entities() *entities.Entities {
	if g == nil {
		return nil
	}

	return g.all
}

func (g *generation) layerGroup() *layers.LayerGroup {
	if all := g.entities(); all != nil {
		return all.LayerGroup
	}

	return nil
}

func (g *generation) auth() authentication.Authentication {
	if all := g.entities(); all != nil {
		return all.Auth
	}

	return nil
}

func (g *generation) analytics() *internalanalytics.AnalyticsWrapper {
	if all := g.entities(); all != nil {
		return all.Analytics
	}

	return nil
}

func (g *generation) tilePathPrefix() string {
	return g.serverCfg.RootPath + g.serverCfg.TilePath
}

func (g *generation) writeHeaders(w http.ResponseWriter) {
	for name, v := range g.serverCfg.Headers {
		w.Header().Add(name, v)
	}

	if !g.serverCfg.Production {
		version, _, _ := static.GetVersionInformation()
		w.Header().Add("X-Powered-By", "tilegroxy "+version)
	}
}

// Shared so each generation is swapped and retired exactly once. Guards which generation is installed. Always locked first
type generationHolder struct {
	current *generation
	mu      sync.RWMutex
}

func newGenerationHolder(gen *generation) *generationHolder {
	return &generationHolder{current: gen}
}

func (h *generationHolder) reload(gen *generation) {
	h.mu.Lock()
	old := h.current
	h.current = gen
	h.mu.Unlock()

	old.markClosing(pkg.BackgroundContext(), generationCloseFloor)
}

// Incremented with the read so a concurrent reload either hands over the new generation or defers retiring the old
func (h *generationHolder) acquire() (*generation, func()) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cur := h.current

	if cur == nil {
		return cur, func() {}
	}

	cur.acquire()

	return cur, cur.release
}

func (h *generationHolder) currentEntities() *entities.Entities {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.current.entities()
}

func (g *generation) acquire() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.refs++
}

func (g *generation) release() {
	g.mu.Lock()

	g.refs--
	shouldClose := g.closing && g.refs <= 0 && !g.closed
	ctx := g.closeCtx

	g.mu.Unlock()

	if shouldClose {
		if err := g.closeNow(ctx); err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("Error releasing entities from the previous configuration: %v", err))
		}
	}
}

// Closes once idle or when the last request returns. The floor covers the gap between reading the pointer and incrementing
func (g *generation) markClosing(ctx context.Context, floor time.Duration) {
	if g == nil {
		return
	}

	g.mu.Lock()
	g.closing = true
	g.closeCtx = ctx
	g.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "Unexpected panic while closing a superseded configuration!", "panic", r, "stack", string(debug.Stack()))
			}
		}()

		time.Sleep(floor)

		g.mu.Lock()
		shouldClose := g.refs <= 0 && !g.closed
		g.mu.Unlock()

		if shouldClose {
			if err := g.closeNow(ctx); err != nil {
				slog.WarnContext(ctx, fmt.Sprintf("Error releasing entities from the previous configuration: %v", err))
			}
		}
	}()
}

// At most once. Late callers wait for an underway close so closeAll never moves on while analytics is draining
func (g *generation) closeNow(ctx context.Context) error {
	if ctx == nil {
		ctx = pkg.BackgroundContext()
	}

	g.mu.Lock()

	if g.closed {
		done := g.done
		g.mu.Unlock()

		select {
		case <-done:
		case <-ctx.Done():
		}

		return nil
	}

	g.closed = true
	g.closes++
	done := g.done
	onClosed := g.onClosed

	g.mu.Unlock()

	err := g.all.Close(ctx)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Error releasing entities from the previous configuration: %v", err))
	} else {
		slog.InfoContext(ctx, "Released entities from the previous configuration")
	}

	close(done)

	if onClosed != nil {
		onClosed()
	}

	return err
}

// Finished, not merely started. Reads done since the closed flag flips before all.Close runs
func (g *generation) isClosed() bool {
	g.mu.Lock()
	done := g.done
	g.mu.Unlock()

	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (g *generation) closeCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.closes
}

func (g *generation) inFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.refs
}

// Lets shutdown release every generation, including a swapped-out one still draining
type generationRegistry struct {
	mu   sync.Mutex
	live map[*generation]struct{}
}

func newGenerationRegistry() *generationRegistry {
	return &generationRegistry{live: make(map[*generation]struct{})}
}

func (r *generationRegistry) add(g *generation) {
	// The hook must precede visibility or a close in between would strand it in live. Locks are sequential, never nested
	g.mu.Lock()
	g.onClosed = func() { r.remove(g) }
	g.mu.Unlock()

	r.mu.Lock()
	r.live[g] = struct{}{}
	r.mu.Unlock()
}

func (r *generationRegistry) remove(g *generation) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.live, g)
}

// For tests asserting closed generations don't accumulate
func (r *generationRegistry) liveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.live)
}

// Bounds how far closeAll can overshoot a drained generation
const inFlightPollInterval = 25 * time.Millisecond

// Runs after the HTTP server drains. Waits for in-flight requests, bounded by ctx, rather than tearing pools from under them
func (r *generationRegistry) closeAll(ctx context.Context) error {
	r.mu.Lock()
	gens := make([]*generation, 0, len(r.live))

	for g := range r.live {
		gens = append(gens, g)
	}

	r.mu.Unlock()

	var errs error

	for _, g := range gens {
		waitForIdle(ctx, g)

		if n := g.inFlight(); n > 0 {
			slog.WarnContext(ctx, fmt.Sprintf("Closing a generation with %v requests still in flight", n))
		}

		errs = errors.Join(errs, g.closeNow(ctx))
		r.remove(g)
	}

	return errs
}

func waitForIdle(ctx context.Context, g *generation) {
	if g.inFlight() <= 0 {
		return
	}

	ticker := time.NewTicker(inFlightPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if g.inFlight() <= 0 {
				return
			}
		}
	}
}
