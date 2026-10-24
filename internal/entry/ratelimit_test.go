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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/Michad/tilegroxy/pkg"
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
        - segment: [tenant]
          limit: 100000
          calendar: month
          overrides:
            - tenants: [blocked]
              limit: 0
layers:
  - id: satellite
    ratelimit: premium
    provider: {name: static, color: FFF}
  - id: counties
    pattern: counties_{year}
    provider: {name: static, color: FFF}
  - id: internal_debug
    skipratelimit: true
    provider: {name: static, color: FFF}
`

func loadRateLimitConfig(t *testing.T, raw string) *config.Config {
	t.Helper()

	cfg, err := configload.LoadConfig(raw)
	require.NoError(t, err)

	return &cfg
}

func runStatus(t *testing.T, opts RateLimitStatusOptions) (string, string, error) {
	t.Helper()

	var out, errOut bytes.Buffer
	err := RateLimitStatus(loadRateLimitConfig(t, rateLimitConfig), opts, &out, &errOut)

	return out.String(), errOut.String(), err
}

func runReset(t *testing.T, opts RateLimitResetOptions) (string, string, error) {
	t.Helper()

	var out, errOut bytes.Buffer
	err := RateLimitReset(loadRateLimitConfig(t, rateLimitConfig), opts, &out, &errOut)

	return out.String(), errOut.String(), err
}

func runList(t *testing.T, raw string, opts RateLimitListOptions) (string, error) {
	t.Helper()

	var out bytes.Buffer
	err := RateLimitList(loadRateLimitConfig(t, raw), opts, &out)

	return out.String(), err
}

// Counts requests against the shared store the same way the server would
func countRequests(t *testing.T, n int, user, tenant, ip, layer string) {
	t.Helper()

	cfg := loadRateLimitConfig(t, rateLimitConfig)
	limiter, err := ratelimit.ConstructLimiter(context.Background(), *cfg, nil, pkgratelimit.RateLimiterDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/tiles/l/1/2/3", nil)
	req.RemoteAddr = ip + ":1"
	ctx := pkg.NewRequestContext(req)
	pkg.SetIdentity(ctx, user, tenant)

	for range n {
		require.Equal(t, ratelimit.Allowed, limiter.Check(ctx, layer).Outcome)
	}
}

func Test_RateLimitStatus(t *testing.T) {
	out, errOut, err := runStatus(t, RateLimitStatusOptions{Request: ratelimit.Request{User: "statususer", Tenant: "blocked"}, LayerNames: []string{"satellite", "counties_2020", "internal_debug"}})
	require.NoError(t, err)
	assert.Equal(t, "Warning: Skipping standard.ip.60s since it needs --ip\n", errOut, "warnings are only given once")
	assert.Contains(t, out, "satellite: rejected by premium.tenant.month, won't be allowed until the limit changes\n")
	assert.Contains(t, out, "counties_2020: allowed\n")

	jsonOut, _, err := runStatus(t, RateLimitStatusOptions{Request: ratelimit.Request{IP: "10.9.0.3"}, LayerNames: []string{"counties_2020"}, All: true, JSON: true})
	require.NoError(t, err)
	assert.Contains(t, jsonOut, `"name":"counties_2020","layer":"counties"`)
	assert.Contains(t, out, "internal_debug: allowed, not rate limited\n")
	assert.Contains(t, out, "satellite    premium.tenant.month    0       0        0")
	assert.NotContains(t, out, "standard.user.1s", "rules that aren't blocking are left out")

	out, _, err = runStatus(t, RateLimitStatusOptions{Request: ratelimit.Request{User: "other", IP: "10.9.0.2"}, LayerNames: []string{"satellite"}})
	require.NoError(t, err)
	assert.Equal(t, "satellite: allowed\n", out)

	out, _, err = runStatus(t, RateLimitStatusOptions{Request: ratelimit.Request{User: "other", IP: "10.9.0.2"}, LayerNames: []string{"satellite"}, All: true})
	require.NoError(t, err)
	assert.Contains(t, out, "standard.ip.60s")
	assert.Contains(t, out, "premium.tenant.month")

	out, _, err = runStatus(t, RateLimitStatusOptions{Request: ratelimit.Request{Tenant: "blocked", IP: "10.9.0.1"}, LayerNames: []string{"satellite"}, JSON: true})
	require.NoError(t, err)

	var parsed []layerStatus
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	require.Len(t, parsed, 1)
	assert.Equal(t, "satellite", parsed[0].Name)
	assert.Equal(t, "satellite", parsed[0].Layer)
	assert.False(t, parsed[0].Allowed)
	assert.Nil(t, parsed[0].RetryAfter)
	assert.Equal(t, "premium", parsed[0].Policy)
	require.Len(t, parsed[0].Rules, 1)
	assert.Equal(t, "premium.tenant.month", parsed[0].Rules[0].Rule)
}

func Test_RateLimitStatus_Errors(t *testing.T) {
	_, _, err := runStatus(t, RateLimitStatusOptions{})
	require.ErrorContains(t, err, "--layer")

	_, _, err = runStatus(t, RateLimitStatusOptions{LayerNames: []string{"nope"}})
	require.ErrorContains(t, err, "invalid layer name: nope")

	_, _, err = runStatus(t, RateLimitStatusOptions{LayerNames: []string{"counties"}})
	require.ErrorContains(t, err, "invalid layer name", "a pattern layer is matched by name, not ID")

	var out, errOut bytes.Buffer
	err = RateLimitStatus(loadRateLimitConfig(t, `
ratelimit:
  name: memory
  policies:
    - id: a
layers:
  - id: l
    provider: {name: static, color: FFF}
`), RateLimitStatusOptions{LayerNames: []string{"l"}}, &out, &errOut)
	require.ErrorIs(t, err, ratelimit.ErrNotInspectable)
}

func Test_RateLimitReset(t *testing.T) {
	countRequests(t, 7, "", "resettenant", "10.9.1.1", "satellite")

	opts := RateLimitResetOptions{Rule: "premium.tenant.month", Segments: map[string]string{"tenant": "resettenant"}, DryRun: true}

	out, _, err := runReset(t, opts)
	require.NoError(t, err)
	assert.Equal(t, "Would reset premium.tenant.month for tenant=\"resettenant\", which had 7 used\n", out)

	opts.DryRun = false
	out, _, err = runReset(t, opts)
	require.NoError(t, err)
	assert.Equal(t, "Reset premium.tenant.month for tenant=\"resettenant\", which had 7 used\n", out)

	opts.JSON = true
	out, _, err = runReset(t, opts)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rule": "premium.tenant.month", "segments": {"tenant": "resettenant"}, "used": 0, "dryRun": false}`, out)

	countRequests(t, 2, "", "", "10.9.1.1", "satellite")

	out, _, err = runReset(t, RateLimitResetOptions{Rule: "standard.user.1s", Segments: map[string]string{"user": ""}})
	require.NoError(t, err)
	assert.Contains(t, out, `Reset standard.user.1s for user="", which had `)
}

func Test_RateLimitReset_Errors(t *testing.T) {
	_, _, err := runReset(t, RateLimitResetOptions{Rule: "premium.tenant.month"})
	require.ErrorContains(t, err, "--tenant is required")

	_, _, err = runReset(t, RateLimitResetOptions{Rule: "nope"})
	require.ErrorContains(t, err, "unknown rate limit rule")
}

func Test_RateLimitList(t *testing.T) {
	out, err := runList(t, rateLimitConfig, RateLimitListOptions{})
	require.NoError(t, err)
	assert.Equal(t, `Rule                    Limit     Period    Overrides    Layers
standard.user.1s        50        1s        0            satellite, counties
standard.ip.60s         100       1m0s      0            satellite, counties
premium.tenant.month    100000    month     1            satellite
`, out)

	out, err = runList(t, rateLimitConfig, RateLimitListOptions{RuleFilter: ratelimit.RuleFilter{Layers: []string{"counties"}}})
	require.NoError(t, err)
	assert.NotContains(t, out, "premium")

	out, err = runList(t, rateLimitConfig, RateLimitListOptions{RuleFilter: ratelimit.RuleFilter{Layers: []string{"counties_1990"}}, JSON: true})
	require.NoError(t, err)

	var parsed []ratelimit.RuleInfo
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	require.Len(t, parsed, 2)
	assert.Equal(t, int64(60), parsed[1].Period)

	_, err = runList(t, rateLimitConfig, RateLimitListOptions{RuleFilter: ratelimit.RuleFilter{Layers: []string{"nope"}}})
	require.ErrorContains(t, err, "invalid layer name")

	_, err = runList(t, rateLimitConfig, RateLimitListOptions{RuleFilter: ratelimit.RuleFilter{Policies: []string{"nope"}}})
	require.ErrorContains(t, err, "unknown rate limit policy")
}

func Test_RateLimitList_MemoryStore(t *testing.T) {
	out, err := runList(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [ip], limit: 3, period: 60}
`, RateLimitListOptions{})
	require.NoError(t, err)
	assert.Contains(t, out, "a.ip.60s")

	_, err = runList(t, `
ratelimit:
  name: memory
`, RateLimitListOptions{})
	require.Error(t, err)

	_, err = runList(t, `
secret:
  name: nope
`, RateLimitListOptions{})
	require.Error(t, err)
}

func Test_WithLimiter_Errors(t *testing.T) {
	noop := func(context.Context, *ratelimit.Limiter) error { return nil }

	require.Error(t, withLimiter(loadRateLimitConfig(t, `
ratelimit:
  name: memory
`), noop))

	require.Error(t, withLimiter(loadRateLimitConfig(t, `
ratelimit:
  name: redis
  datastore: missing
  policies:
    - id: a
`), noop))

	require.Error(t, withLimiter(loadRateLimitConfig(t, `
secret:
  name: nope
`), noop))

	require.Error(t, withLimiter(loadRateLimitConfig(t, `
datastores:
  - name: nope
    id: used
ratelimit:
  name: redis
  datastore: used
  policies:
    - id: a
`), noop))
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
	retry := int64(90)

	var out bytes.Buffer
	require.NoError(t, writeStatuses(&out, []layerStatus{
		{Name: "satellite", Status: ratelimit.Status{Layer: "satellite", Policy: "premium", Allowed: true, Rules: []ratelimit.Usage{
			{Rule: "standard.user.1s", Used: 12, Limit: 50, Remaining: 38, Resets: now.Add(500 * time.Millisecond), Rolling: true},
			{Rule: "premium.tenant.month", Used: 812345, Limit: 5000000, Remaining: 4187655, Resets: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
			{Rule: "premium.ip.day", Used: 3, Limit: -1, Unlimited: true, Resets: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
		}}},
		{Name: "basemap", Status: ratelimit.Status{Layer: "basemap", Policy: "standard", RetryAfter: &retry, Rules: []ratelimit.Usage{
			{Rule: "standard.all.day", Used: 10000, Limit: 10000, Resets: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), Blocking: true},
		}}},
	}, now))

	assert.Equal(t, `satellite: allowed
basemap: rejected by standard.all.day, retry after 1m30s

Layer        Rule                    Used      Limit        Remaining    Resets
satellite    standard.user.1s        12        50           38           1s
satellite    premium.tenant.month    812345    5000000      4187655      2026-11-01T00:00:00Z
satellite    premium.ip.day          3         unlimited    -            2026-10-25T00:00:00Z
basemap      standard.all.day        10000     10000        0            2026-10-25T00:00:00Z
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
