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

package authentications

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// existingRequiredFuncs supplies the minimum validate a custom auth script needs to initialize, so
// close-specific tests don't have to restate it.
const existingRequiredFuncs = `
func validate(token string) tilegroxy.ValidationResult {
	return tilegroxy.ValidationResult{Pass: true, Expiration: time.Now().Add(time.Hour), UserID: "user"}
}
`

const customAuthScriptHeader = `
package custom

import (
	"context"
	"os"
	"time"

	"tilegroxy/tilegroxy"
)
`

func testCustomAuthConfig(script string) CustomConfig {
	cfg := CustomConfig{Script: customAuthScriptHeader + script}
	cfg.Token = map[string]string{ExtractModeHeader: "Authorization"}
	return cfg
}

func buildCustomAuthFromScript(t *testing.T, body string) *Custom {
	t.Helper()

	msgs := config.DefaultConfig().Error.Messages

	a, err := CustomRegistration{}.Initialize(testCustomAuthConfig(body), authentication.AuthenticationDeps{ErrorMessages: msgs})
	require.NoError(t, err)
	require.NotNil(t, a)

	return a.(*Custom)
}

func Test_CustomAuthCloseInvokesScript(t *testing.T) {
	out := filepath.Join(t.TempDir(), "closed.txt")

	script := `
func close(ctx context.Context) error {
	return os.WriteFile("` + out + `", []byte("closed"), 0600)
}
` + existingRequiredFuncs

	a := buildCustomAuthFromScript(t, script)

	require.NoError(t, a.Close(pkg.BackgroundContext()))

	content, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "closed", string(content))
}

func Test_CustomAuthCloseOptionalWhenAbsent(t *testing.T) {
	// Scripts written before this existed must keep working untouched.
	a := buildCustomAuthFromScript(t, existingRequiredFuncs)

	require.NoError(t, a.Close(pkg.BackgroundContext()))
}

func Test_CustomAuthCloseWrongSignatureFailsAtInitialize(t *testing.T) {
	script := `
func close() {}
` + existingRequiredFuncs

	msgs := config.DefaultConfig().Error.Messages

	a, err := CustomRegistration{}.Initialize(testCustomAuthConfig(script), authentication.AuthenticationDeps{ErrorMessages: msgs})

	assert.Nil(t, a)
	require.Error(t, err)
}

func Test_CustomAuthValidate_PopulatesUserAndTenant(t *testing.T) {
	script := `
func validate(token string) tilegroxy.ValidationResult {
	return tilegroxy.ValidationResult{Pass: true, Expiration: time.Now().Add(time.Hour), UserID: "user-1", TenantID: "tenant-1"}
}
`
	a := buildCustomAuthFromScript(t, script)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)
	require.NoError(t, err)
	req.Header["Authorization"] = []string{"sometoken"}

	ctx := pkg.BackgroundContext()
	require.True(t, a.CheckAuthentication(ctx, req))

	userID, _ := pkg.UserIDFromContext(ctx)
	tenantID, _ := pkg.TenantIDFromContext(ctx)
	assert.Equal(t, "user-1", *userID)
	assert.Equal(t, "tenant-1", *tenantID)
}

func Test_CustomAuthValidate_TenantOptional(t *testing.T) {
	// A script that never sets TenantID must leave the context's tenant at its default, not panic
	// or write a zero-value placeholder that reads as "authenticated but tenantless" incorrectly.
	script := `
func validate(token string) tilegroxy.ValidationResult {
	return tilegroxy.ValidationResult{Pass: true, Expiration: time.Now().Add(time.Hour), UserID: "user-1"}
}
`
	a := buildCustomAuthFromScript(t, script)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)
	require.NoError(t, err)
	req.Header["Authorization"] = []string{"sometoken"}

	ctx := pkg.BackgroundContext()
	require.True(t, a.CheckAuthentication(ctx, req))

	tenantID, _ := pkg.TenantIDFromContext(ctx)
	assert.Empty(t, *tenantID)
}

func Test_CustomAuthValidate_WrongSignatureFailsAtInitialize(t *testing.T) {
	// The old tuple-return signature is no longer accepted; it must fail at startup with a clear
	// error rather than silently miscompiling or panicking at request time.
	script := `
func validate(token string) (bool, time.Time, string, []string) {
	return true, time.Now().Add(time.Hour), "user", nil
}
`
	msgs := config.DefaultConfig().Error.Messages

	a, err := CustomRegistration{}.Initialize(testCustomAuthConfig(script), authentication.AuthenticationDeps{ErrorMessages: msgs})

	assert.Nil(t, a)
	require.Error(t, err)
}
