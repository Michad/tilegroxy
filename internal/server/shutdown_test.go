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
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ShutdownBudgetSharesOneDeadline(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Timeout = 30
	cfg.Server.DrainDelay = 5

	budget := newShutdownBudget(&cfg)

	// An unset budget covers both phases that spend it: the drain wait plus a full-length request.
	assert.Equal(t, 35*time.Second, budget.effective())
	assert.Equal(t, 5*time.Second, budget.drainDelay)

	ctx, cancel := budget.context(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok, "budget context must carry a deadline so no phase can block forever")
	assert.WithinDuration(t, time.Now().Add(35*time.Second), deadline, time.Second)
}

func Test_ShutdownBudgetExplicitOverridesTimeout(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Timeout = 300
	cfg.Server.ShutdownTimeout = 20

	budget := newShutdownBudget(&cfg)

	// A long request timeout must not drag shutdown past the orchestrator grace period.
	assert.Equal(t, 20*time.Second, budget.effective())
}

func Test_ShutdownRunsPhasesInOrder(t *testing.T) {
	var order []string
	var mu sync.Mutex

	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()

		order = append(order, name)
	}

	cfg := config.DefaultConfig()
	cfg.Server.Timeout = 5
	cfg.Server.DrainDelay = 0

	phases := shutdownPhases{
		drain:       func() { record("drain") },
		server:      func(context.Context) error { record("server"); return nil },
		generations: func(context.Context) error { record("generations"); return nil },
		health:      func(context.Context) error { record("health"); return nil },
		otel:        func(context.Context) error { record("otel"); return nil },
		logs:        func() { record("logs") },
	}

	require.NoError(t, runShutdown(context.Background(), newShutdownBudget(&cfg), phases))

	// Readiness must fail before draining, health must stop before entities close so its check
	// tickers can't hit a closed pool, and log handles close last so every prior phase can log.
	assert.Equal(t, []string{"drain", "server", "health", "generations", "otel", "logs"}, order)
}

// Health checks run on background tickers that call into caches and providers. If they are still
// running when entities close they fire against a released pool, which surfaces as a recovered
// panic logged on every clean container stop. This models that with a ticker of its own rather
// than asserting phase names, so a reordering fails on the consequence and not just the label.
func Test_ShutdownStopsHealthChecksBeforeReleasingEntities(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Timeout = 5
	cfg.Server.DrainDelay = 0

	var mu sync.Mutex
	entitiesClosed := false
	checkedAfterClose := false

	stopChecks := make(chan struct{})
	checksDone := make(chan struct{})

	go func() {
		defer close(checksDone)

		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stopChecks:
				return
			case <-ticker.C:
				mu.Lock()
				if entitiesClosed {
					checkedAfterClose = true
				}
				mu.Unlock()
			}
		}
	}()

	phases := shutdownPhases{
		drain:  func() {},
		server: func(context.Context) error { return nil },
		health: func(context.Context) error {
			close(stopChecks)
			<-checksDone

			return nil
		},
		generations: func(context.Context) error {
			// Give any still-running ticker a chance to observe the closed state.
			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			entitiesClosed = true
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			return nil
		},
		otel: func(context.Context) error { return nil },
		logs: func() {},
	}

	require.NoError(t, runShutdown(context.Background(), newShutdownBudget(&cfg), phases))

	mu.Lock()
	defer mu.Unlock()

	assert.False(t, checkedAfterClose, "a health check ran after entities were released")
}

func Test_ShutdownStopsWhenBudgetExpires(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.ShutdownTimeout = 1
	cfg.Server.DrainDelay = 0

	reached := false

	phases := shutdownPhases{
		drain: func() {},
		server: func(ctx context.Context) error {
			<-ctx.Done() // a hung upstream that never drains
			return ctx.Err()
		},
		generations: func(context.Context) error { reached = true; return nil },
		health:      func(context.Context) error { return nil },
		otel:        func(context.Context) error { return nil },
		logs:        func() {},
	}

	start := time.Now()
	err := runShutdown(context.Background(), newShutdownBudget(&cfg), phases)

	require.Error(t, err, "an expired budget must surface, not be swallowed")
	assert.Less(t, time.Since(start), 5*time.Second, "shutdown must not outlive its budget")
	assert.False(t, reached, "phases after the expired one are skipped")
}

func Test_ShutdownAlwaysClosesLogs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.ShutdownTimeout = 1
	cfg.Server.DrainDelay = 0

	logsClosed := false

	phases := shutdownPhases{
		drain:       func() {},
		server:      func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		generations: func(context.Context) error { return nil },
		health:      func(context.Context) error { return nil },
		otel:        func(context.Context) error { return nil },
		logs:        func() { logsClosed = true },
	}

	_ = runShutdown(context.Background(), newShutdownBudget(&cfg), phases)

	// Log handles are outside the budget: they cannot block and must outlive the other phases.
	assert.True(t, logsClosed)
}
