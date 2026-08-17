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
	"fmt"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// Wraps around an arbitrary struct for communicating with an arbitrary database. The goal for this datastore mechanism isn't to provide a uniform interface for querying databases but instead to provide a consistent way to declare database connection (pools) that can be reused between providers
type DatastoreWrapper interface {
	// Returns the ID of the datastore as defined in configuration.  Should return cfg.ID
	GetID() string
	// Returns the underlying library for connecting to the datastore.  e.g. for postgresql it returns *pgx.Pool. Calling providers should ensure the type matches what they expect and return a clear error if not (in case the operator mixes things up in config)
	Native() any
}

// DatastoreDeps carries everything a datastore is given at construction. New dependencies are added as
// fields so the Initialize signature stays stable
type DatastoreDeps struct {
	Secreter      secret.Secreter
	ErrorMessages config.ErrorMessages
}

type DatastoreWrapperRegistration interface {
	Name() string
	Initialize(config any, deps DatastoreDeps) (DatastoreWrapper, error)
	InitializeConfig() any
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]DatastoreWrapperRegistration)

func RegisterDatastoreWrapper(reg DatastoreWrapperRegistration) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[reg.Name()] = reg
}

func RegisteredDatastoreWrapper(name string) (DatastoreWrapperRegistration, bool) {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	o, ok := registrations[name]
	return o, ok
}

func RegisteredDatastoreWrapperNames() []string {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

func ConstructDatastoreWrapper(rawConfig map[string]interface{}, deps DatastoreDeps) (DatastoreWrapper, error) {
	rawConfig = pkg.ReplaceEnv(rawConfig)

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := RegisteredDatastoreWrapper(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := config.DecodeEntityConfig(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			return reg.Initialize(cfg, deps)
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "datastore.name", nameCoerce, RegisteredDatastoreWrapperNames())
}
