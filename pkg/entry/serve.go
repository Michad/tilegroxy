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
	"io"

	"github.com/Michad/tilegroxy/internal/server"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type ServeOptions struct {
}

func Serve(cfg *config.Config, _ ServeOptions, _ io.Writer, reloadPtr *func(*config.Config) error) error {
	layerObjects, auth, err := configToEntities(*cfg)
	if err != nil {
		return err
	}

	var nextReloadPtr func(*config.Config, *layer.LayerGroup, authentication.Authentication) error

	reloadCallback := func(newCfg *config.Config) error {
		if nextReloadPtr != nil {
			layerObjects2, auth2, err := configToEntities(*newCfg)
			if err != nil {
				return err
			}

			return nextReloadPtr(newCfg, layerObjects2, auth2)
		}

		return nil
	}

	*reloadPtr = reloadCallback

	err = server.ListenAndServe(cfg, layerObjects, auth, &nextReloadPtr)
	return err
}
