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

package secret

import (
	"sync"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSecreterConfig struct {
	Value string
}

type stubSecreter struct {
	value string
}

func (s stubSecreter) Lookup(_ string) (string, error) {
	return s.value, nil
}

type stubSecreterRegistration struct{}

func (stubSecreterRegistration) Name() string          { return "stub-secreter" }
func (stubSecreterRegistration) InitializeConfig() any { return stubSecreterConfig{} }
func (stubSecreterRegistration) Initialize(cfgAny any, _ SecreterDeps) (Secreter, error) {
	cfg := cfgAny.(stubSecreterConfig)
	return stubSecreter{value: cfg.Value}, nil
}

func init() {
	RegisterSecreter(stubSecreterRegistration{})
}

func Test_ConstructSecreter_UnknownNameErrors(t *testing.T) {
	_, err := ConstructSecreter(map[string]interface{}{"name": "not-a-real-secreter"}, SecreterDeps{ErrorMessages: config.ErrorMessages{EnumError: "%v %v %v"}})
	require.Error(t, err)
}

func Test_ConstructSecreter_ConstructsRegisteredSecreter(t *testing.T) {
	s, err := ConstructSecreter(map[string]interface{}{"name": "stub-secreter", "value": "hunter2"}, SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	require.NotNil(t, s)

	val, err := s.Lookup("anything")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", val)
}

func Test_ConstructSecreter_ReplacesEnvInRawConfig(t *testing.T) {
	t.Setenv("STUB_SECRETER_VALUE", "from-env")

	s, err := ConstructSecreter(map[string]interface{}{"name": "stub-secreter", "value": "env.STUB_SECRETER_VALUE"}, SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	val, err := s.Lookup("anything")
	require.NoError(t, err)
	assert.Equal(t, "from-env", val)
}

func Test_RegisteredSecreterNames_IncludesRegistered(t *testing.T) {
	assert.Contains(t, RegisteredSecreterNames(), "stub-secreter")
}

func Test_RegisterSecreter_ConcurrentIsRaceFree(t *testing.T) {
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RegisterSecreter(stubSecreterRegistration{})
			_ = RegisteredSecreterNames()
			_, _ = RegisteredSecreter("stub-secreter")
		}()
	}
	wg.Wait()

	assert.Contains(t, RegisteredSecreterNames(), "stub-secreter")
}
