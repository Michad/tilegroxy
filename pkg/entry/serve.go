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
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/internal/server"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type ServeOptions struct {
	// Rereads the configuration from its original source.
	ReloadConfig func() (config.Config, error)
	// Receives hot reloads. One is created when nil.
	Reloader *Reloader
}

// Windows never delivers SIGHUP, so signal-driven reload is Unix only
var reloadSignals = []os.Signal{syscall.SIGHUP}

// Serve runs until interrupted. Prefer ServeOptions.Reloader over reloadPtr, which is written without synchronization.
func Serve(cfg *config.Config, opts ServeOptions, out io.Writer, reloadPtr *func(*config.Config) error) error {
	reloader := opts.Reloader
	if reloader == nil {
		reloader = NewReloader()
	}

	reloader.start(cfg)
	defer reloader.stop()

	if reloadPtr != nil {
		*reloadPtr = reloader.Reload
	}

	ready := make(chan struct{})

	if opts.ReloadConfig != nil {
		stop := watchReloadSignal(ready, opts.ReloadConfig, reloader.apply, out)
		defer stop()
	}

	ent, err := configToEntities(pkg.BackgroundContext(), *cfg, reloader.requestLive)
	if err != nil {
		return err
	}

	return server.ListenAndServe(cfg, ent, func(swap swapFunc) {
		reloader.ready(swap)
		close(ready)
	})
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
