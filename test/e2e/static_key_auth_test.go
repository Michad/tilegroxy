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

//go:build e2e

package e2e

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// staticKeyAutoConfig omits authentication.key so tilegroxy generates one at startup and logs it,
// which is the only way an operator (or this test) learns what it is.
const staticKeyAutoConfig = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

var generatedKeyPattern = regexp.MustCompile(`Generated authentication key: (\S+)`)

// extractGeneratedKey scrapes the auto-generated static key out of the child's captured log
// output, matching the exact message static_key.go emits via slog.
func extractGeneratedKey(t *testing.T, output string) string {
	t.Helper()

	match := generatedKeyPattern.FindStringSubmatch(output)
	require.Lenf(t, match, 2, "did not find generated authentication key in output:\n%s", output)

	return match[1]
}

func Test_StaticKeyAuth_GeneratedKeyFromLogsGrantsAccess(t *testing.T) {
	inst := Start(t, Config{Raw: staticKeyAutoConfig})

	key := extractGeneratedKey(t, inst.Output())

	inst.GetWithHeader("/tiles/color/8/12/32", "Authorization", "Bearer "+key).
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "image/png")
}

func Test_StaticKeyAuth_MissingOrWrongKeyIsUnauthorized(t *testing.T) {
	inst := Start(t, Config{Raw: staticKeyAutoConfig})

	// Confirm a key was actually generated, so a passing assertion below isn't accidentally
	// exercising an auth scheme that let everything through regardless.
	extractGeneratedKey(t, inst.Output())

	inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusUnauthorized)
	inst.GetWithHeader("/tiles/color/8/12/32", "Authorization", "Bearer wrongkey").
		ExpectStatus(http.StatusUnauthorized)
}
