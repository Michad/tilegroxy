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

package datastore

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/stretchr/testify/require"
)

// A registration whose Initialize never fails, so registry-level ID validation is what's under
// test here rather than any particular datastore implementation's connection behavior.
type stubDatastoreConfig struct {
	ID string
}

type stubDatastoreWrapper struct {
	id string
}

func (s stubDatastoreWrapper) GetID() string { return s.id }
func (s stubDatastoreWrapper) Native() any   { return nil }

type stubDatastoreRegistration struct{}

func (stubDatastoreRegistration) Name() string          { return "stub" }
func (stubDatastoreRegistration) InitializeConfig() any { return stubDatastoreConfig{} }
func (stubDatastoreRegistration) Initialize(cfgAny any, _ secret.Secreter, _ config.ErrorMessages) (DatastoreWrapper, error) {
	cfg := cfgAny.(stubDatastoreConfig)
	return stubDatastoreWrapper{id: cfg.ID}, nil
}

func init() {
	RegisterDatastoreWrapper(stubDatastoreRegistration{})
}

// Duplicate IDs would otherwise overwrite each other in the registry map, hiding the first
// datastore with no error anywhere.
func Test_ConstructDatastoreRegistry_DuplicateIDErrors(t *testing.T) {
	cfg := []map[string]interface{}{
		{"name": "stub", "id": "dupe"},
		{"name": "stub", "id": "dupe"},
	}

	_, err := ConstructDatastoreRegistry(cfg, nil, config.ErrorMessages{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate datastore id")
}

// Two datastores with no ID would both key into datastores[""].
func Test_ConstructDatastoreRegistry_EmptyIDErrors(t *testing.T) {
	cfg := []map[string]interface{}{
		{"name": "stub"},
	}

	_, err := ConstructDatastoreRegistry(cfg, nil, config.ErrorMessages{})
	require.Error(t, err)
}

func Test_ConstructDatastoreRegistry_UniqueIDsWork(t *testing.T) {
	cfg := []map[string]interface{}{
		{"name": "stub", "id": "one"},
		{"name": "stub", "id": "two"},
	}

	reg, err := ConstructDatastoreRegistry(cfg, nil, config.ErrorMessages{})
	require.NoError(t, err)

	_, ok := reg.Get("one")
	require.True(t, ok)
	_, ok = reg.Get("two")
	require.True(t, ok)
}
