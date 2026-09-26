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

package tg

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/internal/server"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
)

type ServeOptions struct {
	// ReloadConfig re-reads the configuration from its original source. When set, SIGHUP triggers a reload
	ReloadConfig func() (config.Config, error)
}

// Windows never delivers SIGHUP, so signal-driven reload is Unix only
var reloadSignals = []os.Signal{syscall.SIGHUP}

func Serve(cfg *config.Config, opts ServeOptions, _ io.Writer, reloadPtr *func(*config.Config) error) error {
	var nextReloadPtr func(*config.Config, *entities.Entities) error

	tracker := &configTracker{current: cfg}

	reload := newReloadCallback(&nextReloadPtr, tracker)
	*reloadPtr = func(newCfg *config.Config) error {
		return reload(newCfg, audit.ReasonConfigFile)
	}

	ent, err := configToEntities(pkg.BackgroundContext(), *cfg, tracker.secretReload(reload))
	if err != nil {
		return err
	}

	if opts.ReloadConfig != nil {
		stop := watchReloadSignal(opts.ReloadConfig, reload)
		defer stop()
	}

	return server.ListenAndServe(cfg, ent, &nextReloadPtr)
}

// One goroutine and a one-slot channel serialize reloads and collapse signals received mid-reload into one follow-up
func watchReloadSignal(load func() (config.Config, error), reload func(*config.Config, string) error) func() {
	sigs := make(chan os.Signal, 1)
	done := make(chan struct{})

	signal.Notify(sigs, reloadSignals...)

	go func() {
		for {
			select {
			case <-done:
				return
			case <-sigs:
				reloadFromSource(load, reload)
			}
		}
	}()

	return func() {
		signal.Stop(sigs)
		close(done)
	}
}

func reloadFromSource(load func() (config.Config, error), reload func(*config.Config, string) error) {
	ctx := pkg.BackgroundContext()

	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("config reload panic: %v\n%s", r, debug.Stack())
			audit.ConfigReload(ctx, audit.ReasonSignal, err)
			slog.ErrorContext(ctx, "Failed to reload configuration: "+err.Error())
		}
	}()

	slog.InfoContext(ctx, "Reloading configuration because a reload signal was received")

	newCfg, err := load()
	if err != nil {
		audit.ConfigReload(ctx, audit.ReasonSignal, err)
	} else {
		err = reload(&newCfg, audit.ReasonSignal)
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to reload configuration: "+err.Error())
	}
}

// A rotated secret re-resolves whichever config is live, which is not always the one this process
// started with
type configTracker struct {
	mu      sync.Mutex
	current *config.Config
}

func (t *configTracker) set(cfg *config.Config) {
	t.mu.Lock()
	t.current = cfg
	t.mu.Unlock()
}

func (t *configTracker) get() *config.Config {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.current
}

func (t *configTracker) secretReload(reload func(*config.Config, string) error) func(string) {
	return func(reason string) {
		if err := reload(t.get(), reason); err != nil {
			slog.Error("Failed to reload after a secret changed: " + err.Error())
		}
	}
}

func newReloadCallback(nextReloadPtr *func(*config.Config, *entities.Entities) error, tracker *configTracker) func(*config.Config, string) error {
	var reload func(*config.Config, string) error

	reload = func(newCfg *config.Config, reason string) (err error) {
		auditCtx := pkg.BackgroundContext()

		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("config reload panic: %v\n%s", r, debug.Stack())
				audit.ConfigReload(auditCtx, reason, err)
			}
		}()

		if *nextReloadPtr == nil {
			return nil
		}

		ent2, err := configToEntities(auditCtx, *newCfg, tracker.secretReload(reload))
		if err != nil {
			audit.ConfigReload(auditCtx, reason, err)
			return err
		}

		if err := (*nextReloadPtr)(newCfg, ent2); err != nil {
			audit.ConfigReload(auditCtx, reason, err)

			closeCtx, cancel := context.WithTimeout(context.Background(), time.Duration(newCfg.Server.EffectiveShutdownTimeout())*time.Second) // #nosec G115 -- operator-supplied timeout in seconds, far below int64 overflow range
			defer cancel()

			if closeErr := ent2.Close(closeCtx); closeErr != nil {
				slog.WarnContext(closeCtx, fmt.Sprintf("Error releasing entities from a failed reload: %v", closeErr))
			}

			return err
		}

		tracker.set(newCfg)
		audit.ConfigReload(auditCtx, reason, nil)

		return nil
	}

	return reload
}
