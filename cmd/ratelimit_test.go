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

package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runRateLimitCommand(t *testing.T, args ...string) string {
	t.Helper()

	exitStatus = -1
	rootCmd.ResetFlags()
	rateLimitCmd.ResetFlags()
	initRoot()
	initRateLimit()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs(append([]string{"ratelimit"}, args...))
	require.NoError(t, rootCmd.Execute())

	return b.String()
}

func Test_RateLimitCommand_MemoryStoreRejected(t *testing.T) {
	out := runRateLimitCommand(t, "--raw-config", `{"ratelimit": {"name": "memory", "policies": [{"id": "a"}]}}`)

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out, "can't be read or reset from outside the server")
}

func Test_RateLimitCommand_DryRunNeedsReset(t *testing.T) {
	out := runRateLimitCommand(t, "--raw-config", `{}`, "--dry-run")

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out, "--dry-run can only be used with --reset")
}

func Test_RateLimitCommand_BadConfig(t *testing.T) {
	out := runRateLimitCommand(t, "-c", "does-not-exist.yml")

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out, "Error:")
}

func Test_RateLimitCommand_HelpMatchesDocs(t *testing.T) {
	out := runRateLimitCommand(t, "-h")

	docs, err := os.ReadFile("../docs/operation/modules/ROOT/pages/commands/ratelimit.adoc")
	require.NoError(t, err)

	_, block, ok := strings.Cut(string(docs), ".Output of `tilegroxy ratelimit -h`\n[,text]\n----\n")
	require.True(t, ok)
	block, _, _ = strings.Cut(block, "----")

	assert.True(t, strings.HasPrefix(out, block), "update commands/ratelimit.adoc to match:\n%v", out)
}
