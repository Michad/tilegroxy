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

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
)

// Share of the budget reserved for flushing batched analytics, as 1/fraction for integer math
const flushReserveFraction = 5

// Minimum reserve in case the budget is very low
const flushReserveFloor = 2 * time.Second

// The deadline every teardown phase draws from
type shutdownBudget struct {
	total        time.Duration
	drainDelay   time.Duration
	flushReserve time.Duration
}

func newShutdownBudget(cfg *config.Config) shutdownBudget {
	total := time.Duration(configload.EffectiveShutdownTimeout(cfg.Server)) * time.Second // #nosec G115 -- operator-supplied timeout in seconds, far below int64 overflow range

	reserve := total / flushReserveFraction
	if reserve < flushReserveFloor {
		reserve = flushReserveFloor
	}
	// Otherwise earlier phases would get a zero or negative deadline
	if reserve > total {
		reserve = total
	}

	return shutdownBudget{
		total:        total,
		drainDelay:   time.Duration(cfg.Server.DrainDelay) * time.Second, // #nosec G115 -- operator-supplied delay in seconds, far below int64 overflow range
		flushReserve: reserve,
	}
}

func (b shutdownBudget) effective() time.Duration {
	return b.total
}

// Shared by every phase. Callers must cancel it
func (b shutdownBudget) context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, b.total)
}

func (b shutdownBudget) preFlushContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, b.total-b.flushReserve)
}

func (b shutdownBudget) flushContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, b.flushReserve)
}

// In run order. Separate fields rather than a slice keep the ordering constraints readable at the call site
type shutdownPhases struct {
	drain       func()
	server      func(context.Context) error
	generations func(context.Context) error
	health      func(context.Context) error
	otel        func(context.Context) error
	logs        func()
}

func runShutdown(parent context.Context, budget shutdownBudget, phases shutdownPhases) error {
	ctx, cancel := budget.context(parent)
	defer cancel()

	slog.InfoContext(ctx, fmt.Sprintf("Shutting down, budget %v (generations reserved %v)", budget.effective(), budget.flushReserve))

	defer phases.logs()

	phases.drain()

	if budget.drainDelay > 0 {
		select {
		case <-time.After(budget.drainDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var errs []error

	// Reports whether the phase's context is still live. logCtx is only for logging; each phase has its own deadline
	runStep := func(logCtx, stepCtx context.Context, name string, fn func(context.Context) error) bool {
		start := time.Now()
		err := fn(stepCtx)

		slog.InfoContext(logCtx, fmt.Sprintf("Shutdown phase %v took %v", name, time.Since(start)))

		if err != nil {
			errs = append(errs, fmt.Errorf("shutdown phase %v: %w", name, err))
		}

		if stepCtx.Err() != nil {
			slog.WarnContext(logCtx, fmt.Sprintf("Shutdown budget expired during %v", name))
			errs = append(errs, stepCtx.Err())

			return false
		}

		return true
	}

	preFlushCtx, preFlushCancel := budget.preFlushContext(parent)
	defer preFlushCancel()

	// Health stops before generations or its tickers would hit closed pools. Readiness has reported 503 since draining
	okToContinue := runStep(ctx, preFlushCtx, "server", phases.server) &&
		runStep(ctx, preFlushCtx, "health", phases.health)

	// Always runs since it's the only path that flushes analytics. Uses its own reserve rather than the expired preFlushCtx
	flushCtx, flushCancel := budget.flushContext(parent)
	defer flushCancel()

	okToContinue = runStep(ctx, flushCtx, "generations", phases.generations) && okToContinue

	if okToContinue {
		runStep(ctx, ctx, "otel", phases.otel)
	}

	return errors.Join(errs...)
}
