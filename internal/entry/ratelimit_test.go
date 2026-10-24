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

package entry

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/Michad/tilegroxy/pkg/config"
	pkgratelimit "github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A shared store the command can read, standing in for redis
type mapStore struct {
	mu     sync.Mutex
	counts map[string]int64
}

var sharedMapStore = &mapStore{counts: map[string]int64{}}

func mapKey(c pkgratelimit.RateLimitCounter) string {
	return c.Key + "@" + strconv.FormatInt(c.Window.Unix(), 10)
}

func (s *mapStore) Get(_ context.Context, counters []pkgratelimit.RateLimitCounter) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]int64, len(counters))
	for i, c := range counters {
		result[i] = s.counts[mapKey(c)]
	}

	return result, nil
}

func (s *mapStore) Increment(_ context.Context, counters []pkgratelimit.RateLimitCounter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range counters {
		s.counts[mapKey(c)]++
	}

	return nil
}

func (s *mapStore) Reset(_ context.Context, counters []pkgratelimit.RateLimitCounter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range counters {
		delete(s.counts, mapKey(c))
	}

	return nil
}

type mapStoreRegistration struct{}

func (mapStoreRegistration) Name() string          { return "entrytestmap" }
func (mapStoreRegistration) InitializeConfig() any { return struct{ Datastore string }{} }
func (mapStoreRegistration) Initialize(_ any, _ pkgratelimit.RateLimiterDeps) (pkgratelimit.RateLimiter, error) {
	return sharedMapStore, nil
}

func init() {
	pkgratelimit.RegisterRateLimiter(mapStoreRegistration{})
}

const rateLimitConfig = `
datastores:
  - name: memory
    id: used
  - name: nope
    id: unused
authentication:
  name: static key
  key: abc
ratelimit:
  name: entrytestmap
  datastore: used
  policies:
    - id: standard
      rules:
        - {segment: [user], limit: 50, period: 1}
        - {segment: [ip], limit: 100, period: 60}
    - id: premium
      extend: [standard]
      rules:
        - {segment: [tenant], limit: 100000, calendar: month}
layers:
  - id: satellite
    ratelimit: premium
    provider: {name: static, color: FFF}
`

func runRateLimit(t *testing.T, raw string, opts RateLimitOptions) (string, string, error) {
	t.Helper()

	cfg, err := configload.LoadConfig(raw)
	require.NoError(t, err)

	var out, errOut bytes.Buffer
	err = RateLimit(&cfg, opts, &out, &errOut)

	return out.String(), errOut.String(), err
}

func Test_RateLimit_InspectAndReset(t *testing.T) {
	cfg, err := configload.LoadConfig(rateLimitConfig)
	require.NoError(t, err)

	limiter, err := ratelimit.ConstructLimiter(context.Background(), cfg, nil, pkgratelimit.RateLimiterDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	ctx := context.Background()
	usages, _, err := limiter.Inspect(ctx, ratelimit.InspectOptions{Tenant: "acme", User: "alice", IP: "10.0.0.1"})
	require.NoError(t, err)
	require.Len(t, usages, 3)

	_, _, err = limiter.Reset(ctx, ratelimit.InspectOptions{Tenant: "acme"}, false)
	require.NoError(t, err)

	opts := RateLimitOptions{InspectOptions: ratelimit.InspectOptions{Policies: []string{"premium"}, Tenant: "acme", User: "alice"}}

	out, errOut, err := runRateLimit(t, rateLimitConfig, opts)
	require.NoError(t, err)
	assert.Contains(t, out, "Rule")
	assert.Contains(t, out, "premium.tenant.month")
	assert.Contains(t, out, "standard.user.1s")
	assert.Contains(t, errOut, "Warning: Skipping standard.ip.60s since it needs --ip")

	opts.JSON = true
	out, _, err = runRateLimit(t, rateLimitConfig, opts)
	require.NoError(t, err)

	var parsed []ratelimit.Usage
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	assert.Len(t, parsed, 2)

	opts = RateLimitOptions{InspectOptions: ratelimit.InspectOptions{Layer: "satellite", Tenant: "acme", IP: "10.0.0.1"}, Reset: true, DryRun: true}
	out, _, err = runRateLimit(t, rateLimitConfig, opts)
	require.NoError(t, err)
	assert.Equal(t, "Would reset standard.user.1s\nWould reset standard.ip.60s\nWould reset premium.tenant.month\n", out)

	opts.DryRun = false
	out, _, err = runRateLimit(t, rateLimitConfig, opts)
	require.NoError(t, err)
	assert.Contains(t, out, "Reset premium.tenant.month")

	opts.JSON = true
	out, _, err = runRateLimit(t, rateLimitConfig, opts)
	require.NoError(t, err)
	assert.JSONEq(t, `{"dryRun": false, "reset": ["standard.user.1s", "standard.ip.60s", "premium.tenant.month"]}`, out)
}

func Test_RateLimit_Errors(t *testing.T) {
	_, _, err := runRateLimit(t, rateLimitConfig, RateLimitOptions{DryRun: true})
	require.Error(t, err)

	_, _, err = runRateLimit(t, rateLimitConfig, RateLimitOptions{InspectOptions: ratelimit.InspectOptions{Policies: []string{"nope"}}})
	require.Error(t, err)

	_, _, err = runRateLimit(t, `
ratelimit:
  name: memory
  policies:
    - id: a
`, RateLimitOptions{})
	require.ErrorIs(t, err, ratelimit.ErrNotInspectable)

	_, _, err = runRateLimit(t, `
ratelimit:
  name: memory
`, RateLimitOptions{})
	require.Error(t, err)

	_, _, err = runRateLimit(t, `
ratelimit:
  name: redis
  datastore: missing
  policies:
    - id: a
`, RateLimitOptions{})
	require.Error(t, err)

	_, _, err = runRateLimit(t, `
secret:
  name: nope
`, RateLimitOptions{})
	require.Error(t, err)
}

func Test_StoreDatastores(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Datastores = []map[string]interface{}{{"name": "memory", "id": "a"}, {"name": "memory", "id": "b"}}

	assert.Nil(t, storeDatastores(&cfg))

	cfg.RateLimit = map[string]interface{}{"name": "redis", "datastore": "b"}
	assert.Equal(t, []map[string]interface{}{{"name": "memory", "id": "b"}}, storeDatastores(&cfg))

	cfg.RateLimit = map[string]interface{}{"name": "redis", "datastore": "c"}
	assert.Nil(t, storeDatastores(&cfg))
}

func Test_WriteUsageTable(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 500000000, time.UTC)

	var out bytes.Buffer
	require.NoError(t, writeUsageTable(&out, []ratelimit.Usage{
		{Rule: "standard.user.1s", Used: 12, Limit: 50, Remaining: 38, Resets: now.Add(500 * time.Millisecond), Rolling: true},
		{Rule: "premium.tenant.month", Used: 812345, Limit: 5000000, Remaining: 4187655, Resets: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{Rule: "premium.ip.day", Used: 3, Limit: -1, Unlimited: true, Resets: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
	}, now))

	assert.Equal(t, `Rule                    Used      Limit        Remaining    Resets
standard.user.1s        12        50           38           1s
premium.tenant.month    812345    5000000      4187655      2026-11-01T00:00:00Z
premium.ip.day          3         unlimited    -            2026-10-25T00:00:00Z
`, out.String())
}

func Test_CheckConfig_ValidatesRateLimit(t *testing.T) {
	// The command never connects to the unused datastore, but a config check does
	cfg, err := configload.LoadConfig(strings.Replace(rateLimitConfig, "  - name: nope\n    id: unused\n", "", 1))
	require.NoError(t, err)
	require.NoError(t, CheckConfig(&cfg, CheckOptions{}, nil))

	cfg, err = configload.LoadConfig(`
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [user], limit: 5, period: 1}
`)
	require.NoError(t, err)
	require.ErrorContains(t, CheckConfig(&cfg, CheckOptions{}, nil), "ratelimit")

	cfg, err = configload.LoadConfig(`
server:
  forwarded:
    trusted: [not-an-ip]
`)
	require.NoError(t, err)
	require.ErrorContains(t, CheckConfig(&cfg, CheckOptions{}, nil), "server.forwarded.trusted")
}

func Test_DiffConfig_RateLimitIsReloadable(t *testing.T) {
	assert.Equal(t, diffImpactReload, sectionImpact("ratelimit"))
	assert.Equal(t, diffImpactReload, sectionImpact("defaultratelimit"))
	assert.Equal(t, diffImpactRestart, sectionImpact("server"))
}
