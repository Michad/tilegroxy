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

const inspectPolicies = `
ratelimit:
  name: memory
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
            - tenants: [acme]
              limit: 5000000
            - tenants: [internal]
              limit: -1
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

func Test_Inspect_ShowsUsage(t *testing.T) {
	l, clock, _ := newInspectLimiter(t)

	ctx := requestContext("alice", "acme", "10.0.0.1:1")
	for range 12 {
		require.Equal(t, Allowed, l.Check(ctx, "satellite").Outcome)
	}

	clock.advance(500 * time.Millisecond)

	usages, warnings, err := l.Inspect(context.Background(), InspectOptions{Policies: []string{"premium"}, Tenant: "acme", User: "alice"})
	require.NoError(t, err)

	assert.Equal(t, []string{"Skipping standard.ip.60s since it needs --ip", "Skipping premium.layer.day since it needs --layer"}, warnings)
	require.Len(t, usages, 2)

	assert.Equal(t, Usage{Rule: "standard.user.1s", Policy: "standard", Used: 12, Limit: 50, Remaining: 38, Resets: clock.t.Add(500 * time.Millisecond), Rolling: true}, usages[0])
	assert.Equal(t, Usage{Rule: "premium.tenant.month", Policy: "premium", Used: 12, Limit: 5000000, Remaining: 4999988, Resets: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}, usages[1])
}

func Test_Inspect_ByLayerAndUnlimited(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	usages, warnings, err := l.Inspect(context.Background(), InspectOptions{Layer: "satellite", Tenant: "internal", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.Empty(t, warnings)

	require.Len(t, usages, 4)
	assert.Equal(t, "premium.tenant.month", usages[2].Rule)
	assert.True(t, usages[2].Unlimited)
	assert.Equal(t, int64(0), usages[2].Remaining)

	usages, _, err = l.Inspect(context.Background(), InspectOptions{Layer: "basemap", IP: "10.0.0.1"})
	require.NoError(t, err)
	require.Len(t, usages, 2)
	assert.Equal(t, "standard.user.1s", usages[0].Rule)
}

func Test_Inspect_AllPoliciesDeduplicated(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	usages, _, err := l.Inspect(context.Background(), InspectOptions{Layer: "basemap", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.Len(t, usages, 2)

	usages, _, err = l.Inspect(context.Background(), InspectOptions{Policies: []string{"premium", "standard"}, Layer: "basemap", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.Len(t, usages, 4)

	usages, warnings, err := l.Inspect(context.Background(), InspectOptions{IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.Len(t, usages, 3)
	assert.Len(t, warnings, 1)
}

func Test_Inspect_Errors(t *testing.T) {
	l, _, _ := newInspectLimiter(t)

	_, _, err := l.Inspect(context.Background(), InspectOptions{Policies: []string{"nope"}})
	require.Error(t, err)

	_, _, err = l.Inspect(context.Background(), InspectOptions{Layer: "internal_debug"})
	require.Error(t, err)

	_, _, err = l.Reset(context.Background(), InspectOptions{Layer: "nope"}, false)
	require.Error(t, err)

	usages, warnings, err := l.Inspect(context.Background(), InspectOptions{Policies: []string{"standard"}})
	require.NoError(t, err)
	assert.Len(t, usages, 1)
	assert.Len(t, warnings, 1)

	_, _, err = l.Inspect(context.Background(), InspectOptions{Layer: "nope"})
	require.Error(t, err)

	l.store = failingStore{getErr: errors.New("down")}
	_, _, err = l.Inspect(context.Background(), InspectOptions{})
	require.ErrorIs(t, err, ErrNotInspectable)

	plain, _ := newTestLimiter(t, inspectPolicies)
	_, _, err = plain.Inspect(context.Background(), InspectOptions{})
	require.ErrorIs(t, err, ErrNotInspectable)

	_, _, err = plain.Reset(context.Background(), InspectOptions{}, true)
	require.ErrorIs(t, err, ErrNotInspectable)
}

func Test_Reset_ClearsCurrentAndPrevious(t *testing.T) {
	l, clock, _ := newInspectLimiter(t)

	ctx := requestContext("alice", "acme", "10.0.0.1:1")
	for range 50 {
		require.Equal(t, Allowed, l.Check(ctx, "basemap").Outcome)
	}

	clock.advance(time.Second)
	require.Equal(t, Rejected, l.Check(ctx, "basemap").Outcome, "the previous bucket still counts")

	opts := InspectOptions{Policies: []string{"standard"}, User: "alice"}

	names, _, err := l.Reset(context.Background(), opts, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"standard.user.1s"}, names)
	assert.Equal(t, Rejected, l.Check(ctx, "basemap").Outcome, "a dry run changes nothing")

	names, _, err = l.Reset(context.Background(), opts, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"standard.user.1s"}, names)
	assert.Equal(t, Allowed, l.Check(ctx, "basemap").Outcome)
}

func Test_Reset_StoreError(t *testing.T) {
	l, _, store := newInspectLimiter(t)
	store.resetErr = errors.New("down")
	l.store = store

	_, _, err := l.Reset(context.Background(), InspectOptions{User: "alice", IP: "1.2.3.4", Layer: "basemap"}, false)
	require.Error(t, err)
}
