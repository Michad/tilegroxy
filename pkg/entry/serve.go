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
	"runtime/debug"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/internal/server"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
)

type ServeOptions struct {
}

func Serve(cfg *config.Config, _ ServeOptions, _ io.Writer, reloadPtr *func(*config.Config) error) error {
	var nextReloadPtr func(*config.Config, *entities.Entities) error

	reload := newReloadCallback(&nextReloadPtr)
	*reloadPtr = reload

	// The watcher holds the config this process started with: a rotated secret re-resolves it, the
	// file is not what changed
	secretReload := func() {
		if err := reload(cfg); err != nil {
			slog.Error("Failed to reload after a secret changed: " + err.Error())
		}
	}

	ent, err := configToEntities(pkg.BackgroundContext(), *cfg, secretReload)
	if err != nil {
		return err
	}

	return server.ListenAndServe(cfg, ent, &nextReloadPtr)
}

func newReloadCallback(nextReloadPtr *func(*config.Config, *entities.Entities) error) func(*config.Config) error {
	var reload func(*config.Config) error

	reload = func(newCfg *config.Config) (err error) {
		auditCtx := pkg.BackgroundContext()

		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("config reload panic: %v\n%s", r, debug.Stack())
				audit.ConfigReload(auditCtx, err)
			}
		}()

		if *nextReloadPtr == nil {
			return nil
		}

		ent2, err := configToEntities(auditCtx, *newCfg, func() {
			if reloadErr := reload(newCfg); reloadErr != nil {
				slog.Error("Failed to reload after a secret changed: " + reloadErr.Error())
			}
		})
		if err != nil {
			audit.ConfigReload(auditCtx, err)
			return err
		}

		if err := (*nextReloadPtr)(newCfg, ent2); err != nil {
			audit.ConfigReload(auditCtx, err)

			closeCtx, cancel := context.WithTimeout(context.Background(), time.Duration(newCfg.Server.EffectiveShutdownTimeout())*time.Second) // #nosec G115 -- operator-supplied timeout in seconds, far below int64 overflow range
			defer cancel()

			if closeErr := ent2.Close(closeCtx); closeErr != nil {
				slog.WarnContext(closeCtx, fmt.Sprintf("Error releasing entities from a failed reload: %v", closeErr))
			}

			return err
		}

		audit.ConfigReload(auditCtx, nil)

		return nil
	}

	return reload
}
