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

package authentication

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubAuthConfig struct {
	Allow bool
}

type stubAuth struct {
	allow bool
}

func (s stubAuth) CheckAuthentication(_ context.Context, _ *http.Request) bool {
	return s.allow
}

type stubAuthRegistration struct{}

func (stubAuthRegistration) Name() string          { return "stub-auth" }
func (stubAuthRegistration) InitializeConfig() any { return stubAuthConfig{} }
func (stubAuthRegistration) Initialize(cfgAny any, _ AuthenticationDeps) (Authentication, error) {
	cfg := cfgAny.(stubAuthConfig)
	return stubAuth{allow: cfg.Allow}, nil
}

func init() {
	RegisterAuthentication(stubAuthRegistration{})
}

func Test_ConstructAuth_UnknownNameErrors(t *testing.T) {
	_, err := ConstructAuth(map[string]interface{}{"name": "not-a-real-auth"}, AuthenticationDeps{ErrorMessages: config.ErrorMessages{EnumError: "%v %v %v"}})
	require.Error(t, err)
}

func Test_ConstructAuth_ConstructsRegisteredAuth(t *testing.T) {
	auth, err := ConstructAuth(map[string]interface{}{"name": "stub-auth", "allow": true}, AuthenticationDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	require.NotNil(t, auth)

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	assert.True(t, auth.CheckAuthentication(context.Background(), req))
}

func Test_ConstructAuth_WrapsWithName(t *testing.T) {
	auth, err := ConstructAuth(map[string]interface{}{"name": "stub-auth", "allow": false}, AuthenticationDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	wrapper, ok := auth.(AuthWrapper)
	require.True(t, ok)
	assert.Equal(t, "stub-auth", wrapper.Name)
}

func Test_RegisteredAuthenticationNames_IncludesRegistered(t *testing.T) {
	assert.Contains(t, RegisteredAuthenticationNames(), "stub-auth")
}

func Test_RegisterAuthentication_ConcurrentIsRaceFree(t *testing.T) {
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RegisterAuthentication(stubAuthRegistration{})
			_ = RegisteredAuthenticationNames()
			_, _ = RegisteredAuthentication("stub-auth")
		}()
	}
	wg.Wait()

	assert.Contains(t, RegisteredAuthenticationNames(), "stub-auth")
}
