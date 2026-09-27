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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The example in examples/extension builds only against pkg and cmd, so a breaking change to either
// fails `make extension` or this test before it reaches a release.
func Test_Extension_TestsThenServesEverySampleEntity(t *testing.T) {
	p := ports{Server: freePort(t), Health: freePort(t)}
	dir := t.TempDir()
	env := []string{
		"SERVER_PORT=" + strconv.Itoa(p.Server),
		"HEALTH_PORT=" + strconv.Itoa(p.Health),
		"SERVER_DRAINDELAY=0",
	}

	inst := launch(t, ExtensionBinaryPath(t), nil, env, dir, p, "")

	// The test command ran first through the sample provider, cache and datastore
	assert.Contains(t, inst.Output(), "Completed with 0 failures")
	assert.Regexp(t, `color\s+Yes\s+Yes\s+Yes\s+None`, inst.Output())

	// The sample authentication takes its key from the sample secret source
	inst.Get("/tiles/color/3/2/1").ExpectStatus(http.StatusUnauthorized)
	inst.GetWithHeader("/tiles/color/3/2/1", "X-Api-Key", "hunter2").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "image/png")

	Until(t, Scale(10*time.Second), "sample health check to pass", func() bool {
		return inst.GetHealth().StatusCode == http.StatusOK
	})

	analyticsPath := filepath.Join(dir, "analytics.log")

	var event map[string]any

	Until(t, Scale(10*time.Second), "sample analytics to record the tile", func() bool {
		b, err := os.ReadFile(analyticsPath)
		if err != nil || len(b) == 0 {
			return false
		}

		return json.Unmarshal([]byte(strings.SplitN(string(b), "\n", 2)[0]), &event) == nil
	})

	assert.Equal(t, "color", event["LayerID"])
	assert.Equal(t, "sample", event["UserID"])

	inst.Signal(os.Interrupt)
	require.Equal(t, 0, inst.WaitExit(Scale(30*time.Second)), inst.Output())
}
