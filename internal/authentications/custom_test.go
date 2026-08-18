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
func validate(token string) (bool, time.Time, string, []string) {
	return true, time.Now().Add(time.Hour), "user", nil
}
`

const customAuthScriptHeader = `
package custom

import (
	"context"
	"os"
	"time"
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
