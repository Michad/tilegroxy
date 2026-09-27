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
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

type stubSecreterConfig struct {
	Value string
}

type stubSecreter struct {
	value string
}

func (s stubSecreter) Lookup(_ context.Context, _ string) (string, string, error) {
	return s.value, "v1", nil
}

func (s stubSecreter) Check(_ context.Context, keys []string) ([]string, error) {
	out := make([]string, len(keys))
	for i := range keys {
		out[i] = "v1"
	}
	return out, nil
}

type stubSecreterRegistration struct{}

func (stubSecreterRegistration) Name() string          { return "stub-secreter" }
func (stubSecreterRegistration) InitializeConfig() any { return stubSecreterConfig{} }
func (stubSecreterRegistration) CheckBatchSize() int   { return 2 }
func (stubSecreterRegistration) Initialize(cfgAny any, _ SecreterDeps) (Secreter, error) {
	cfg := cfgAny.(stubSecreterConfig)
	return stubSecreter{value: cfg.Value}, nil
}

type stubUnwatchableRegistration struct{ stubSecreterRegistration }

func (stubUnwatchableRegistration) Name() string        { return "stub-unwatchable" }
func (stubUnwatchableRegistration) CheckBatchSize() int { return 0 }

func init() {
	RegisterSecreter(stubSecreterRegistration{})
	RegisterSecreter(stubUnwatchableRegistration{})
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
