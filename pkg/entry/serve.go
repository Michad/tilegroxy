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

func Serve(cfg *config.Config, opts ServeOptions, out io.Writer, reloadPtr *func(*config.Config) error) error {
	var nextReloadPtr func(*config.Config, *entities.Entities) error

	tracker := &configTracker{current: cfg}

	reload := newReloadCallback(&nextReloadPtr, tracker)
	serialized := serializeReloads(reload)

	*reloadPtr = func(newCfg *config.Config) error {
		return serialized(newCfg, audit.ReasonConfigFile)
	}

	ready := make(chan struct{})

	if opts.ReloadConfig != nil {
		stop := watchReloadSignal(ready, opts.ReloadConfig, serialized, out)
		defer stop()
	}

	ent, err := configToEntities(pkg.BackgroundContext(), *cfg, tracker.secretReload(reload))
	if err != nil {
		return err
	}

	return server.ListenAndServe(cfg, ent, &nextReloadPtr, func() { close(ready) })
}

// Secret reloads must not use this since a generation's Close waits on its secret pollers
func serializeReloads(reload func(*config.Config, string) error) func(*config.Config, string) error {
	var mu sync.Mutex

	return func(newCfg *config.Config, reason string) error {
		mu.Lock()
		defer mu.Unlock()

		return reload(newCfg, reason)
	}
}

// Signals received before the server is ready wait for it, then collapse with any others into one reload
func watchReloadSignal(ready <-chan struct{}, load func() (config.Config, error), reload func(*config.Config, string) error, out io.Writer) func() {
	sigs := make(chan os.Signal, 1)
	done := make(chan struct{})

	signal.Notify(sigs, reloadSignals...)

	go func() {
		for {
			select {
			case <-done:
				return
			case <-sigs:
			}

			select {
			case <-done:
				return
			case <-ready:
				reloadFromSource(load, reload, out)
			}
		}
	}()

	return func() {
		signal.Stop(sigs)
		close(done)
	}
}

func reloadFromSource(load func() (config.Config, error), reload func(*config.Config, string) error, out io.Writer) {
	ctx := pkg.BackgroundContext()

	report := func(err error) {
		if out != nil {
			fmt.Fprintf(out, "Error: %v\n", err.Error())
		}
	}

	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("config reload panic: %v\n%s", r, debug.Stack())
			audit.ConfigReload(ctx, audit.ReasonSignal, err)
			report(err)
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
		report(err)
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
