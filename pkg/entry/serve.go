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
	"time"

	"github.com/Michad/tilegroxy/internal/server"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
)

type ServeOptions struct {
}

func Serve(cfg *config.Config, _ ServeOptions, _ io.Writer, reloadPtr *func(*config.Config) error) error {
	ent, err := configToEntities(*cfg)
	if err != nil {
		return err
	}

	var nextReloadPtr func(*config.Config, *entities.Entities) error

	*reloadPtr = newReloadCallback(&nextReloadPtr)

	err = server.ListenAndServe(cfg, ent, &nextReloadPtr)
	return err
}

// newReloadCallback builds the callback Serve hands to config's change-watcher. It builds a fresh
// generation and hands it to whatever swap function *nextReloadPtr points at when the reload
// fires. If configToEntities fails nothing was built, so there is nothing to close. If swap fails
// after a successful build, the new generation never reaches a handler that could release it, so
// it's closed here instead - otherwise its connection pools would stay open for the life of the
// process while the old generation keeps serving. The close is bounded by the same shutdown
// timeout used elsewhere so a stuck pool can't hang a reload indefinitely.
func newReloadCallback(nextReloadPtr *func(*config.Config, *entities.Entities) error) func(*config.Config) error {
	return func(newCfg *config.Config) error {
		if *nextReloadPtr == nil {
			return nil
		}

		ent2, err := configToEntities(*newCfg)
		if err != nil {
			return err
		}

		if err := (*nextReloadPtr)(newCfg, ent2); err != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Duration(newCfg.Server.EffectiveShutdownTimeout())*time.Second)
			defer cancel()

			if closeErr := ent2.Close(closeCtx); closeErr != nil {
				slog.WarnContext(closeCtx, fmt.Sprintf("Error releasing entities from a failed reload: %v", closeErr))
			}

			return err
		}

		return nil
	}
}
