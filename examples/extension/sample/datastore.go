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

package sample

import (
	"fmt"
	"sync"

	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

type DatastoreConfig struct {
	ID string
}

// An in-memory map shared by whichever entities reference its ID. A real datastore would hold a connection pool
type Datastore struct {
	DatastoreConfig
	store *sync.Map
}

func init() {
	datastore.RegisterDatastoreWrapper(DatastoreRegistration{})
}

type DatastoreRegistration struct{}

func (DatastoreRegistration) InitializeConfig() any {
	return DatastoreConfig{}
}

func (DatastoreRegistration) Name() string {
	return "sample"
}

func (DatastoreRegistration) Initialize(cfgAny any, deps datastore.DatastoreDeps) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(DatastoreConfig)

	if cfg.ID == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "datastores.id")
	}

	return &Datastore{cfg, &sync.Map{}}, nil
}

func (d *Datastore) GetID() string {
	return d.ID
}

func (d *Datastore) Native() any {
	return d.store
}
