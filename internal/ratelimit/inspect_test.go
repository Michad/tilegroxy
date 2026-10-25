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

package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A memory store that can be reset, standing in for a shared one
type resettableMemory struct {
	*Memory
	resetErr error
}

func (m resettableMemory) Reset(_ context.Context, counters []ratelimit.RateLimitCounter) error {
	for _, c := range counters {
		m.counters.remove(memoryKey(c))
	}

	return m.resetErr
}

// A failing store that can be reset, so the read failure is reached
type failingResetter struct {
	failingStore
}

func (failingResetter) Reset(_ context.Context, _ []ratelimit.RateLimitCounter) error {
	return nil
}

const inspectPolicies = `
ratelimit:
  name: memory
  policies:
    - id: standard
      rules:
        - {segment: [user], limit: 50, period: 1}
        - segment: [ip]
          limit: 100
          period: 60
          overrides:
            - ips: [192.168.0.0/16]
              users: [carol, bob]
              limit: 1000
        - {segment: [], limit: 10000, calendar: day}
    - id: premium
      extend: [standard]
      rules:
        - segment: [tenant]
          limit: 100000
          calendar: month
          overrides:
            - tenants: [acme]
              limit: 5000000
            - tenants: [internal]
              limit: -1
            - tenants: [delinquent]
              limit: 0
        - {segment: [layer], limit: 1000, calendar: day}
defaultratelimit: standard
` + testAuth + testLayers

func newInspectLimiter(t *testing.T) (*Limiter, *testClock, resettableMemory) {
	t.Helper()

	l, clock := newTestLimiter(t, inspectPolicies)
	store := resettableMemory{Memory: l.store.(*Memory)}
	l.store = store

	return l, clock, store
}

func ruleNames(usages []Usage) []string {
	names := make([]string, 0, len(usages))
	for _, u := range usages {
		names = append(names, u.Rule)
	}

	return names
}

func Test_Status_Allowed(t *testing.T) {
	l, clock, _ := newInspectLimiter(t)

	ctx := requestContext("alice", "acme", "10.0.0.1:1")
	for range 12 {
		require.Equal(t, Allowed, l.Check(ctx, "satellite").Outcome)
	}

	clock.advance(500 * time.Millisecond)

	status, warnings, err := l.Status(context.Background(), "satellite", Request{Tenant: "acme", User: "alice"})
	require.NoError(t, err)

	assert.Equal(t, []string{"Skipping standard.ip.60s since it needs --ip"}, warnings)
	assert.True(t, status.Allowed)
	assert.Equal(t, "premium", status.Policy)
	assert.Nil(t, status.RetryAfter)
	assert.Equal(t, []string{"standard.user.1s", "standard.all.day", "premium.tenant.month", "premium.layer.day"}, ruleNames(status.Rules))

	assert.Equal(t, Usage{Rule: "standard.user.1s", Policy: "standard", Used: 12, Limit: 50, Remaining: 38, Resets: clock.t.Add(500 * time.Millisecond), Rolling: true}, status.Rules[0])
	assert.Equal(t, Usage{Rule: "premium.tenant.month", Policy: "premium", Used: 12, Limit: 5000000, Remaining: 4999988, Resets: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}, status.Rules[2])
}

func Test_Status_Rejected(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	ctx := requestContext("alice", "acme", "10.0.0.1:1")
	for range 50 {
		require.Equal(t, Allowed, l.Check(ctx, "basemap").Outcome)
	}

	status, warnings, err := l.Status(context.Background(), "basemap", Request{User: "alice", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.False(t, status.Allowed)
	require.NotNil(t, status.RetryAfter)
	assert.Equal(t, int64(1), *status.RetryAfter)
	assert.True(t, status.Rules[0].Blocking)
	assert.False(t, status.Rules[1].Blocking)

	status, _, err = l.Status(context.Background(), "basemap", Request{User: "bob", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.True(t, status.Allowed, "other users have their own counter")
}

func Test_Status_OverrideLimits(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	status, _, err := l.Status(context.Background(), "satellite", Request{Tenant: "delinquent", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.False(t, status.Allowed)
	assert.Nil(t, status.RetryAfter, "a limit of 0 never allows a request")
	assert.True(t, status.Rules[3].Blocking)

	status, _, err = l.Status(context.Background(), "satellite", Request{Tenant: "internal", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.True(t, status.Allowed)
	assert.True(t, status.Rules[3].Unlimited)
	assert.False(t, status.Rules[3].Blocking)
	assert.Equal(t, int64(0), status.Rules[3].Remaining)

	status, _, err = l.Status(context.Background(), "basemap", Request{IP: "192.168.1.1"})
	require.NoError(t, err)
	assert.Equal(t, int64(1000), status.Rules[1].Limit)
}

func Test_Status_NotLimited(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	for _, layer := range []string{"internal_debug", "nope"} {
		status, warnings, err := l.Status(context.Background(), layer, Request{})
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.Equal(t, Status{Layer: layer, Allowed: true, Rules: []Usage{}}, status)
	}

	ipOnly, _ := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [ip], limit: 3, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)
	ipOnly.store = resettableMemory{Memory: ipOnly.store.(*Memory)}

	status, warnings, err := ipOnly.Status(context.Background(), "l", Request{})
	require.NoError(t, err)
	assert.Len(t, warnings, 1)
	assert.True(t, status.Allowed)
	assert.Empty(t, status.Rules)
}

func Test_Status_Errors(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	l.store = failingResetter{failingStore{getErr: errors.New("down")}}
	_, _, err := l.Status(context.Background(), "basemap", Request{})
	require.ErrorContains(t, err, "down")

	l.store = failingStore{}
	_, _, err = l.Status(context.Background(), "basemap", Request{})
	require.ErrorIs(t, err, ErrNotInspectable)
}

func Test_ResetRule_ClearsCurrentAndPrevious(t *testing.T) {
	l, clock, _ := newInspectLimiter(t)

	ctx := requestContext("alice", "acme", "10.0.0.1:1")
	for range 50 {
		require.Equal(t, Allowed, l.Check(ctx, "basemap").Outcome)
	}

	clock.advance(time.Second)
	require.Equal(t, Rejected, l.Check(ctx, "basemap").Outcome, "the previous bucket still counts")

	segments := map[string]string{SegmentUser: "alice"}

	used, warnings, err := l.ResetRule(context.Background(), "standard.user.1s", segments, true)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, int64(50), used)
	assert.Equal(t, Rejected, l.Check(ctx, "basemap").Outcome, "a dry run changes nothing")

	used, _, err = l.ResetRule(context.Background(), "standard.user.1s", segments, false)
	require.NoError(t, err)
	assert.Equal(t, int64(50), used)
	assert.Equal(t, Allowed, l.Check(ctx, "basemap").Outcome)
}

func Test_ResetRule_Anonymous(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	anonymous := requestContext("", "", "10.0.0.1:1")
	for range 50 {
		require.Equal(t, Allowed, l.Check(anonymous, "basemap").Outcome)
	}

	require.Equal(t, Rejected, l.Check(anonymous, "basemap").Outcome)

	used, _, err := l.ResetRule(context.Background(), "standard.user.1s", map[string]string{SegmentUser: ""}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(50), used)
	assert.Equal(t, Allowed, l.Check(anonymous, "basemap").Outcome)
}

func Test_ResetRule_Segments(t *testing.T) {
	l, _, _ := newInspectLimiter(t)
	ctx := context.Background()

	_, _, err := l.ResetRule(ctx, "premium.tenant.month", map[string]string{}, false)
	require.EqualError(t, err, "rule premium.tenant.month is segmented by tenant, so --tenant is required")

	_, _, err = l.ResetRule(ctx, "premium.tenant.month", map[string]string{SegmentTenant: "acme", SegmentUser: "alice"}, false)
	require.EqualError(t, err, "rule premium.tenant.month isn't segmented by user, so --user can't be used")

	_, _, err = l.ResetRule(ctx, "premium.nope.month", map[string]string{}, false)
	require.ErrorContains(t, err, "unknown rate limit rule")

	_, _, err = l.ResetRule(ctx, "standard.all.day", map[string]string{}, false)
	require.NoError(t, err)

	_, warnings, err := l.ResetRule(ctx, "premium.layer.day", map[string]string{SegmentLayer: "satellite"}, false)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	_, warnings, err = l.ResetRule(ctx, "premium.layer.day", map[string]string{SegmentLayer: "basemap"}, true)
	require.NoError(t, err)
	assert.Equal(t, []string{`Layer "basemap" isn't limited by premium.layer.day`}, warnings)
}

func Test_ResetRule_Errors(t *testing.T) {
	l, _, store := newInspectLimiter(t)
	segments := map[string]string{SegmentUser: "alice"}

	store.resetErr = errors.New("down")
	l.store = store

	_, _, err := l.ResetRule(context.Background(), "standard.user.1s", segments, false)
	require.ErrorContains(t, err, "down")

	l.store = failingResetter{failingStore{getErr: errors.New("unreachable")}}
	_, _, err = l.ResetRule(context.Background(), "standard.user.1s", segments, false)
	require.ErrorContains(t, err, "unreachable")

	plain, _ := newTestLimiter(t, inspectPolicies)
	_, _, err = plain.ResetRule(context.Background(), "standard.user.1s", segments, true)
	require.ErrorIs(t, err, ErrNotInspectable)
}

func Test_Rules_Filters(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	names := func(filter RuleFilter) []string {
		t.Helper()

		rules, err := l.Rules(filter)
		require.NoError(t, err)

		result := make([]string, 0, len(rules))
		for _, r := range rules {
			result = append(result, r.Rule)
		}

		return result
	}

	all := []string{"standard.user.1s", "standard.ip.60s", "standard.all.day", "premium.tenant.month", "premium.layer.day"}

	assert.Equal(t, all, names(RuleFilter{}))
	assert.Equal(t, []string{"premium.tenant.month"}, names(RuleFilter{Search: "TENANT"}))
	assert.Equal(t, all[:3], names(RuleFilter{Policies: []string{"standard"}}))
	assert.Equal(t, all, names(RuleFilter{Policies: []string{"premium"}}))
	assert.Equal(t, all[:3], names(RuleFilter{Layers: []string{"basemap"}}))
	assert.Equal(t, all, names(RuleFilter{Layers: []string{"basemap", "satellite"}}))
	assert.Empty(t, names(RuleFilter{Layers: []string{"internal_debug"}}))
	assert.Equal(t, []string{"standard.ip.60s", "premium.layer.day"}, names(RuleFilter{Segments: []string{"IP", "layer"}}))
	assert.Equal(t, []string{"standard.all.day"}, names(RuleFilter{Segments: []string{"all"}}))
	assert.Equal(t, []string{"standard.all.day"}, names(RuleFilter{Search: "day", Policies: []string{"standard"}}))

	_, err := l.Rules(RuleFilter{Policies: []string{"nope"}})
	require.ErrorContains(t, err, "unknown rate limit policy")

	_, err = l.Rules(RuleFilter{Segments: []string{"nope"}})
	require.ErrorContains(t, err, "unknown segment")
}

func Test_Rules_Describe(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	rules, err := l.Rules(RuleFilter{})
	require.NoError(t, err)
	require.Len(t, rules, 5)

	assert.Equal(t, RuleInfo{
		Rule:     "standard.ip.60s",
		Policy:   "standard",
		Segments: []string{"ip"},
		Period:   60,
		Limit:    100,
		Overrides: []OverrideInfo{
			{Users: []string{"bob", "carol"}, IPs: []string{"192.168.0.0/16"}, Limit: 1000},
		},
		Layers: []string{"basemap", "satellite"},
	}, rules[1])

	assert.Equal(t, RuleInfo{
		Rule:      "standard.all.day",
		Policy:    "standard",
		Segments:  []string{},
		Calendar:  "day",
		Limit:     10000,
		Overrides: []OverrideInfo{},
		Layers:    []string{"basemap", "satellite"},
	}, rules[2])

	assert.Equal(t, []OverrideInfo{
		{Tenants: []string{"acme"}, Limit: 5000000},
		{Tenants: []string{"internal"}, Limit: -1},
		{Tenants: []string{"delinquent"}, Limit: 0},
	}, rules[3].Overrides)
	assert.Equal(t, []string{"satellite"}, rules[3].Layers)
}

func Test_ConstructPolicies_SkipsStore(t *testing.T) {
	cfg := loadConfig(t, `
ratelimit:
  name: redis
  datastore: missing
  policies:
    - id: a
      rules:
        - {segment: [ip], limit: 3, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)

	_, err := ConstructLimiter(context.Background(), cfg, nil, ratelimit.RateLimiterDeps{ErrorMessages: cfg.Error.Messages})
	require.Error(t, err)

	l, err := ConstructPolicies(context.Background(), cfg, nil, cfg.Error.Messages)
	require.NoError(t, err)
	assert.False(t, l.Enabled())

	rules, err := l.Rules(RuleFilter{})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, []string{"l"}, rules[0].Layers)

	_, _, err = l.Status(context.Background(), "l", Request{IP: "10.0.0.1"})
	require.ErrorIs(t, err, ErrNotInspectable)

	cfg = loadConfig(t, `
ratelimit:
  name: memory
  policies:
    - id: "bad id"
`)
	_, err = ConstructPolicies(context.Background(), cfg, nil, cfg.Error.Messages)
	require.Error(t, err)
}
