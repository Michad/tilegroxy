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

package datastore

import (
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

type DatastoreRegistry struct {
	datastores map[string]DatastoreWrapper
}

func (reg DatastoreRegistry) Get(id string) (DatastoreWrapper, bool) {
	res, ok := reg.datastores[id]
	return res, ok
}

func ConstructDatastoreRegistry(cfg []map[string]interface{}, secreter secret.Secreter, errorMessages config.ErrorMessages) (*DatastoreRegistry, error) {
	reg := DatastoreRegistry{}
	reg.datastores = make(map[string]DatastoreWrapper)

	for _, curCfg := range cfg {
		curCfg = pkg.ReplaceEnv(curCfg)
		curCfg, err := pkg.ReplaceConfigValues(curCfg, "secret", secreter.Lookup)

		if err != nil {
			return nil, err
		}

		wrapper, err := ConstructDatastoreWrapper(curCfg, secreter, errorMessages)

		if err != nil {
			return nil, err
		}

		reg.datastores[wrapper.GetID()] = wrapper
	}

	return &reg, nil
}
