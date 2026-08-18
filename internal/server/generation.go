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
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities"
)

// How long a generation waits before trusting its refcount. The handler increments under the same
// lock that guards the pointer read, so this only covers the window between those two operations
const generationCloseFloor = 2 * time.Second

// generation is one constructed set of entities plus the count of requests still using it. A reload
// swaps the pointer and marks the outgoing generation closing; it releases once the last request returns
type generation struct {
	all *entities.Entities

	mu       sync.Mutex
	refs     int
	closing  bool
	closed   bool
	closes   int
	closeCtx context.Context //nolint:containedctx // carries the shutdown deadline to a close that happens on whichever goroutine drops the last reference

	// onClosed lets the registry drop its reference once the close finishes, so superseded
	// generations don't accumulate for the life of the process. Nil until registry.add sets it
	onClosed func()

	// done is closed once closeNow's call to all.Close returns, so a second caller racing an
	// in-progress close can wait for the drain instead of treating "closed" as "finished"
	done chan struct{}
}

func newGeneration(ent *entities.Entities) *generation {
	return &generation{all: ent, done: make(chan struct{})}
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

// markClosing retires the generation. It closes immediately once idle, or when the last in-flight
// request returns. The floor covers the gap between a handler reading the pointer and incrementing
func (g *generation) markClosing(ctx context.Context, floor time.Duration) {
	g.mu.Lock()
	g.closing = true
	g.closeCtx = ctx
	g.mu.Unlock()

	go func() {
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

// closeNow closes the underlying entities at most once. A caller that arrives after a close is
// already underway waits for it to finish instead of returning as if nothing needed waiting for,
// so closeAll can never move on while an analytics batcher is still draining
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

// isClosed reports whether the close has finished, not merely started. It reads done rather than
// the closed flag because closed flips true before all.Close runs, while a drain is still pending
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

// generationRegistry owns every live generation so shutdown can release all of them, including one a
// recent reload swapped out that has not finished draining
type generationRegistry struct {
	mu   sync.Mutex
	live map[*generation]struct{}
}

func newGenerationRegistry() *generationRegistry {
	return &generationRegistry{live: make(map[*generation]struct{})}
}

func (r *generationRegistry) add(g *generation) {
	// Install the removal hook before the generation becomes visible, otherwise a close that
	// starts in between would leave it stranded in live. The two locks are taken in sequence,
	// never nested, so there is no ordering to invert.
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

// liveCount reports how many generations the registry currently retains. Used by tests to assert
// closed generations don't accumulate for the life of the process.
func (r *generationRegistry) liveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.live)
}

// inFlightPollInterval bounds how long closeAll can overshoot a drained generation while waiting
// for its refcount to reach zero
const inFlightPollInterval = 25 * time.Millisecond

// closeAll releases every live generation. Called on shutdown, after the HTTP server has drained, so
// stragglers are closed even if their refcount never reached zero. Waits for in-flight requests to
// finish, bounded by ctx, rather than tearing down connection pools out from under them
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

// waitForIdle blocks until g has no in-flight requests or ctx is done, whichever comes first
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
