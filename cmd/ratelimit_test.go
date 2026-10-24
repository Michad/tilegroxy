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

const rateLimitTestConfig = `{
	"authentication": {"name": "static key", "key": "abc"},
	"ratelimit": {"name": "memory", "policies": [{"id": "a", "rules": [
		{"segment": ["tenant"], "limit": 5, "calendar": "month"},
		{"segment": ["ip"], "limit": 5, "period": 60}
	]}]},
	"layers": [{"id": "l", "provider": {"name": "static", "color": "FFF"}}]
}`

func runRateLimitCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	exitStatus = -1
	rootCmd.ResetFlags()
	rateLimitStatusCmd.ResetFlags()
	rateLimitResetCmd.ResetFlags()
	rateLimitListCmd.ResetFlags()
	initRoot()
	initRateLimitStatus()
	initRateLimitReset()
	initRateLimitList()

	b := bytes.NewBufferString("")
	rootCmd.SetOut(b)
	rootCmd.SetErr(b)
	rootCmd.SetArgs(append([]string{"ratelimit"}, args...))
	err := rootCmd.Execute()

	return b.String(), err
}

func Test_RateLimitStatusCommand_MemoryStoreRejected(t *testing.T) {
	out, err := runRateLimitCommand(t, "status", "--raw-config", rateLimitTestConfig, "-l", "l", "--tenant", "acme", "--user", "alice", "--ip", "10.0.0.1", "-a", "--json")
	require.NoError(t, err)

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out, "can't be read or reset from outside the server")
}

func Test_RateLimitStatusCommand_InvalidLayer(t *testing.T) {
	out, err := runRateLimitCommand(t, "status", "--raw-config", rateLimitTestConfig, "-l", "nope")
	require.NoError(t, err)

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out, "invalid layer name: nope")
}

func Test_RateLimitResetCommand_MemoryStoreRejected(t *testing.T) {
	out, err := runRateLimitCommand(t, "reset", "a.tenant.month", "--raw-config", rateLimitTestConfig, "--tenant", "acme", "--dry-run", "--json")
	require.NoError(t, err)

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out, "can't be read or reset from outside the server")
}

func Test_RateLimitResetCommand_NeedsRule(t *testing.T) {
	_, err := runRateLimitCommand(t, "reset", "--raw-config", rateLimitTestConfig)
	require.Error(t, err)
}

func Test_RateLimitListCommand(t *testing.T) {
	out, err := runRateLimitCommand(t, "list", "tenant", "--raw-config", rateLimitTestConfig, "-l", "l", "-p", "a", "--segment", "tenant")
	require.NoError(t, err)

	assert.Equal(t, -1, exitStatus)
	assert.Contains(t, out, "a.tenant.month")
	assert.NotContains(t, out, "a.ip.60s")

	out, err = runRateLimitCommand(t, "list", "--raw-config", rateLimitTestConfig, "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"rule":"a.ip.60s"`)

	out, err = runRateLimitCommand(t, "list", "--raw-config", rateLimitTestConfig, "-p", "nope")
	require.NoError(t, err)
	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out, "unknown rate limit policy")
}

func Test_RateLimitCommands_BadConfig(t *testing.T) {
	for _, args := range [][]string{{"status", "-l", "l"}, {"reset", "a.tenant.month"}, {"list"}} {
		out, err := runRateLimitCommand(t, append(args, "-c", "does-not-exist.yml")...)
		require.NoError(t, err)

		assert.Equal(t, 1, exitStatus, args)
		assert.Contains(t, out, "Error:", args)
	}
}

func Test_RateLimitCommands_HelpMatchesDocs(t *testing.T) {
	for _, sub := range []string{"status", "reset", "list"} {
		out, err := runRateLimitCommand(t, sub, "-h")
		require.NoError(t, err)

		docs, err := os.ReadFile("../docs/operation/modules/ROOT/pages/commands/ratelimit_" + sub + ".adoc")
		require.NoError(t, err)

		_, block, ok := strings.Cut(string(docs), ".Output of `tilegroxy ratelimit "+sub+" -h`\n[,text]\n----\n")
		require.True(t, ok, sub)
		block, _, _ = strings.Cut(block, "----")

		assert.True(t, strings.HasPrefix(out, block), "update commands/ratelimit_%v.adoc to match:\n%v", sub, out)
	}
}
