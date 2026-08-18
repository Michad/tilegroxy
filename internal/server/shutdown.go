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
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
)

// shutdownBudget is the single deadline every teardown phase draws from. Phases share it rather than
// each taking a fresh timeout, so the total cannot exceed what the orchestrator allows before SIGKILL
type shutdownBudget struct {
	total      time.Duration
	drainDelay time.Duration
}

func newShutdownBudget(cfg *config.Config) shutdownBudget {
	return shutdownBudget{
		total:      time.Duration(cfg.Server.EffectiveShutdownTimeout()) * time.Second,
		drainDelay: time.Duration(cfg.Server.DrainDelay) * time.Second,
	}
}

func (b shutdownBudget) effective() time.Duration {
	return b.total
}

// context returns the deadline shared by every phase. Callers must cancel it
func (b shutdownBudget) context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, b.total)
}

// shutdownPhases are the teardown steps in the order they run. Each is a separate field rather than a
// slice so the ordering constraints stay readable at the call site
type shutdownPhases struct {
	drain       func()
	server      func(context.Context) error
	generations func(context.Context) error
	health      func(context.Context) error
	otel        func(context.Context) error
	logs        func()
}

// runShutdown executes the teardown phases against one shared deadline. A phase that exhausts the
// budget stops the sequence, since every later phase would fail on the same expired context. Log
// handles close last and outside the budget so the phases above can still write
func runShutdown(parent context.Context, budget shutdownBudget, phases shutdownPhases) error {
	ctx, cancel := budget.context(parent)
	defer cancel()

	slog.InfoContext(ctx, fmt.Sprintf("Shutting down, budget %v", budget.effective()))

	defer phases.logs()

	phases.drain()

	// Returning here would skip the server drain entirely, dropping in-flight requests. Config
	// validation keeps the delay under the budget, so this only fires on an already-dead context
	if budget.drainDelay > 0 {
		select {
		case <-time.After(budget.drainDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"server", phases.server},
		// Health goes before generations because its check tickers call into caches and providers.
		// Left running they would fire against a closed pool and log a panic on every clean stop.
		// Readiness has reported 503 since the drain phase, so the endpoint has no job left here
		{"health", phases.health},
		{"generations", phases.generations},
		{"otel", phases.otel},
	}

	var errs []error

	for _, step := range steps {
		start := time.Now()
		err := step.fn(ctx)

		slog.InfoContext(ctx, fmt.Sprintf("Shutdown phase %v took %v", step.name, time.Since(start)))

		if err != nil {
			errs = append(errs, fmt.Errorf("shutdown phase %v: %w", step.name, err))
		}

		if ctx.Err() != nil {
			slog.WarnContext(ctx, fmt.Sprintf("Shutdown budget expired during %v, skipping remaining phases", step.name))
			errs = append(errs, ctx.Err())

			break
		}
	}

	return errors.Join(errs...)
}
