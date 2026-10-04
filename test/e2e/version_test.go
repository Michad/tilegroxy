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
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What internal/static falls back to when the linker flags didn't land
const (
	unsetVersion = "v0.X.Y"
	unsetRef     = "HEAD"
	unsetDate    = "Unknown"
)

// The reason this harness exists. In-process tests set the variables directly, so they miss broken Makefile injection
func Test_Version_LdflagsAreInjected(t *testing.T) {
	out, code := Run(t, "version", "--json")
	require.Equal(t, 0, code)

	var res map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &res))

	assert.NotEqual(t, unsetVersion, res["version"], "version ldflag did not reach the binary")
	assert.NotEqual(t, unsetRef, res["ref"], "ref ldflag did not reach the binary")
	assert.NotEqual(t, unsetDate, res["buildDate"], "buildDate ldflag did not reach the binary")
}

// Catches injection wired to the wrong value, like swapped version and ref, regardless of which commit built it
func Test_Version_FieldsHaveTheRightShape(t *testing.T) {
	out, code := Run(t, "version", "--json")
	require.Equal(t, 0, code)

	var res map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &res))

	assert.Regexp(t, `^v\d+\.\d+\.\d+`, res["version"])
	assert.Regexp(t, `^[0-9a-f]{7,40}(-dirty)?$`, res["ref"])

	_, err := time.Parse(time.RFC3339, res["buildDate"])
	assert.NoError(t, err, "buildDate should be an RFC3339 timestamp, got %q", res["buildDate"])
}

// Ties the version to a runtime header. Production mode suppresses it, so production must stay false
func Test_Version_PoweredByHeaderCarriesVersion(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	resp := inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)

	powered := resp.Header.Get("X-Powered-By")
	assert.Contains(t, powered, "tilegroxy ")
	assert.NotContains(t, powered, unsetVersion, "header carries the unset version fallback")
}

// The only check that `make docs` output reached the binary. make e2e depends on docs, so this never skips
func Test_Binary_ServesEmbeddedDocumentation(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	inst.Get("/docs/index.html").ExpectStatus(http.StatusOK)
}

// Fails only on binaries built without viper_bind_struct. Map keys like server.headers don't bind, so a scalar is used
func Test_Binary_EnvVarConfigBindingWorks(t *testing.T) {
	inst := Start(t, Config{
		Raw: staticLayerConfig,
		Env: []string{"SERVER_TILEPATH=customtiles"},
	})

	inst.Get("/customtiles/color/8/12/32").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "image/png")

	// The old path is unrouted, and unrouted paths 307 to the docs rather than 404
	inst.GetNoRedirect("/tiles/color/8/12/32").
		ExpectStatus(http.StatusTemporaryRedirect).
		ExpectHeader("Location", "/docs")
}
