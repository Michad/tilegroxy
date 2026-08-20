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

// The values pkg/static falls back to when the linker flags did not land.
const (
	unsetVersion = "v0.X.Y"
	unsetRef     = "HEAD"
	unsetDate    = "Unknown"
)

// Test_Version_LdflagsAreInjected is the test this whole harness exists for. A refactor that breaks
// how the Makefile injects version information leaves every in-process test green, because those
// set the variables directly rather than building a binary.
func Test_Version_LdflagsAreInjected(t *testing.T) {
	out, code := Run(t, "version", "--json")
	require.Equal(t, 0, code)

	var res map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &res))

	assert.NotEqual(t, unsetVersion, res["version"], "version ldflag did not reach the binary")
	assert.NotEqual(t, unsetRef, res["ref"], "ref ldflag did not reach the binary")
	assert.NotEqual(t, unsetDate, res["buildDate"], "buildDate ldflag did not reach the binary")
}

// Shape checking catches injection wired to the wrong value, such as version and ref being swapped,
// which a non-default assertion alone would miss. It stays independent of which commit built the
// binary, so it holds for a release or Docker build too.
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

// X-Powered-By ties the injected version to a runtime response header rather than only the version
// command. The header is suppressed when production is true, so this config must leave it false.
func Test_Version_PoweredByHeaderCarriesVersion(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	resp := inst.Get("/tiles/color/8/12/32").ExpectStatus(http.StatusOK)

	powered := resp.Header.Get("X-Powered-By")
	assert.Contains(t, powered, "tilegroxy ")
	assert.NotContains(t, powered, unsetVersion, "header carries the unset version fallback")
}

// The only check that `make docs` output actually reached the binary. make e2e depends on docs, so
// this is a real assertion rather than a skip.
func Test_Binary_ServesEmbeddedDocumentation(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	inst.Get("/docs/index.html").ExpectStatus(http.StatusOK)
}

// Config binding via environment variable is what the viper_bind_struct build tag enables. A binary
// built without the tag fails here and nowhere else. Keys use _ as the delimiter with no prefix, so
// this targets a scalar; map keys such as server.headers do not bind through AutomaticEnv.
func Test_Binary_EnvVarConfigBindingWorks(t *testing.T) {
	inst := Start(t, Config{
		Raw: staticLayerConfig,
		Env: []string{"SERVER_TILEPATH=customtiles"},
	})

	inst.Get("/customtiles/color/8/12/32").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "image/png")

	// The old path is no longer routed. Unrouted paths do not 404: default_handler.go answers them
	// with a 307 to the docs path, so assert that directly rather than following it.
	inst.GetNoRedirect("/tiles/color/8/12/32").
		ExpectStatus(http.StatusTemporaryRedirect).
		ExpectHeader("Location", "/docs")
}
