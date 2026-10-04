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

package datastores

import (
	"context"
	"errors"
	"fmt"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

type Registry struct {
	datastores map[string]datastore.DatastoreWrapper
}

func (reg *Registry) Get(id string) (datastore.DatastoreWrapper, bool) {
	if reg == nil {
		return nil, false
	}

	res, ok := reg.datastores[id]
	return res, ok
}

// Called on shutdown and after a hot reload swaps in a new generation of entities
func (reg *Registry) Close(ctx context.Context) error {
	if reg == nil {
		return nil
	}

	errs := make([]error, 0, len(reg.datastores))

	for _, ds := range reg.datastores {
		errs = append(errs, lifecycle.CloseIfCloser(ctx, ds))
	}

	return errors.Join(errs...)
}

func ConstructDatastoreRegistry(ctx context.Context, cfg []map[string]interface{}, secreter secret.Secreter, errorMessages config.ErrorMessages) (*Registry, error) {
	var err error
	reg := Registry{}
	reg.datastores = make(map[string]datastore.DatastoreWrapper)

	for _, curCfg := range cfg {
		curCfg = configload.ReplaceEnv(curCfg)
		if secreter != nil {
			curCfg, err = configload.ReplaceConfigValues(curCfg, "secret", func(k string) (string, error) {
				v, _, lookupErr := secreter.Lookup(ctx, k)
				return v, lookupErr
			})

			if err != nil {
				return nil, err
			}
		}

		wrapper, err := ConstructDatastoreWrapper(curCfg, datastore.DatastoreDeps{Secreter: secreter, ErrorMessages: errorMessages})

		if err != nil {
			return nil, err
		}

		id := wrapper.GetID()
		if id == "" {
			return nil, errors.New("datastore is missing a required id")
		}
		if _, exists := reg.datastores[id]; exists {
			return nil, fmt.Errorf("duplicate datastore id %q: every datastore must have a unique id", id)
		}

		reg.datastores[id] = wrapper
	}

	return &reg, nil
}

func ConstructDatastoreWrapper(rawConfig map[string]interface{}, deps datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	rawConfig = configload.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := datastore.RegisteredDatastoreWrapper(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := configload.DecodeEntityConfig(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			return reg.Initialize(cfg, deps)
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "datastore.name", nameCoerce, datastore.RegisteredDatastoreWrapperNames())
}
