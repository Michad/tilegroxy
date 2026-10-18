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
	"sync"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// Not a uniform query interface, just a consistent way to declare connection pools that providers can share
type DatastoreWrapper interface {
	// Should return cfg.ID
	GetID() string
	// The underlying client, e.g. *pgx.Pool. Callers should check the type and return a clear error for operator mixups
	Native() any
}

// New dependencies are added as fields so the Initialize signature stays stable
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

// Gives entities access to the datastores configured by ID
type DatastoreRegistry interface {
	Get(id string) (DatastoreWrapper, bool)
}
