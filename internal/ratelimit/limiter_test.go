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
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testClock struct {
	t time.Time
}

func (c *testClock) now() time.Time {
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.t = c.t.Add(d)
}

// Swaps in a private memory store so tests don't share the package-level counters
func newTestLimiter(t *testing.T, raw string) (*Limiter, *testClock) {
	t.Helper()

	l, err := construct(t, raw)
	require.NoError(t, err)

	clock := &testClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	l.now = clock.now
	l.store = &Memory{counters: newLRU(1000), now: clock.now}

	return l, clock
}

func requestContext(user, tenant, ip string) context.Context {
	req := httptest.NewRequest(http.MethodGet, "/tiles/l/1/2/3", nil)
	req.RemoteAddr = ip

	ctx := pkg.NewRequestContext(req)
	pkg.SetIdentity(ctx, user, tenant)

	return ctx
}

// Exact spelling, since the limiter keeps the draft standard's casing
func header(res Result, name string) string {
	if v := res.Header[name]; len(v) > 0 {
		return v[0]
	}

	return ""
}

func captureAudit(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	return &buf
}

const docsPolicies = `
ratelimit:
  name: memory
  policies:
    - id: standard
      rules:
        - segment:
            - user
          limit: 50
          period: 1
    - id: premium
      extend:
        - standard
      rules:
        - segment:
            - tenant
          limit: 100000
          calendar: month
defaultratelimit: standard
` + testAuth + testLayers

func Test_Check_AllowsUntilLimitThenRejects(t *testing.T) {
	l, _ := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [ip], limit: 3, calendar: hour}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)
	ctx := requestContext("", "", "10.0.0.1:1234")

	for i := range 3 {
		res := l.Check(ctx, "l")
		require.Equal(t, Allowed, res.Outcome, i)
		assert.Equal(t, `"a.ip.hour";q=3;w=3600`, header(res, HeaderRateLimitPolicy))
		assert.Equal(t, `"a.ip.hour";r=`+string(rune('2'-i))+`;t=3600`, header(res, HeaderRateLimit))
		assert.Empty(t, res.Header.Get(HeaderRetryAfter))
	}

	res := l.Check(ctx, "l")
	assert.Equal(t, Rejected, res.Outcome)
	assert.Equal(t, "a.ip.hour", res.Rule)
	assert.Equal(t, "3600", res.Header.Get(HeaderRetryAfter))
	assert.Equal(t, `"a.ip.hour";r=0;t=3600`, header(res, HeaderRateLimit))

	// A different IP has its own counter
	assert.Equal(t, Allowed, l.Check(requestContext("", "", "10.0.0.2:1234"), "l").Outcome)
}

func Test_Check_RejectionsAreNotCounted(t *testing.T) {
	l, clock := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [], limit: 2, period: 10}
        - {segment: [], limit: 5, calendar: day}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)
	ctx := requestContext("", "", "10.0.0.1:1234")

	for range 2 {
		require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
	}

	for range 10 {
		require.Equal(t, Rejected, l.Check(ctx, "l").Outcome)
	}

	// Two buckets later the rolling rule has forgotten everything, and the daily rule only saw the two allowed
	clock.advance(20 * time.Second)

	for range 2 {
		require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
	}

	assert.Equal(t, "a.all.10s", l.Check(ctx, "l").Rule)

	clock.advance(20 * time.Second)

	require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)

	res := l.Check(ctx, "l")
	assert.Equal(t, Rejected, res.Outcome)
	assert.Equal(t, "a.all.day", res.Rule)
}

func Test_Check_RollingWindowWeighsPreviousBucket(t *testing.T) {
	l, clock := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [], limit: 10, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)
	ctx := requestContext("", "", "10.0.0.1:1234")

	for range 10 {
		require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
	}

	// 15 seconds into the next bucket, floor(45/60 of the previous 10) still count
	clock.advance(75 * time.Second)

	for range 3 {
		require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
	}

	res := l.Check(ctx, "l")
	require.Equal(t, Rejected, res.Outcome)
	assert.Equal(t, "3", res.Header.Get(HeaderRetryAfter))
}

func Test_Check_ExtendedRulesShareCounters(t *testing.T) {
	l, _ := newTestLimiter(t, docsPolicies)

	ctx := requestContext("alice", "acme", "10.0.0.1:1234")

	for range 25 {
		require.Equal(t, Allowed, l.Check(ctx, "basemap").Outcome)
	}

	var res Result
	for range 25 {
		res = l.Check(ctx, "satellite")
		require.Equal(t, Allowed, res.Outcome)
	}

	assert.Equal(t, `"standard.user.1s";q=50;w=1, "premium.tenant.month";q=100000;w=2678400`, header(res, HeaderRateLimitPolicy))
	assert.True(t, strings.HasPrefix(header(res, HeaderRateLimit), `"standard.user.1s";r=0;t=1, "premium.tenant.month";r=99975;t=`))

	res = l.Check(ctx, "satellite")
	assert.Equal(t, Rejected, res.Outcome)
	assert.Equal(t, "standard.user.1s", res.Rule)
	assert.Equal(t, "1", res.Header.Get(HeaderRetryAfter))

	assert.Equal(t, Rejected, l.Check(ctx, "basemap").Outcome)

	// Never limited, and no headers
	res = l.Check(ctx, "internal_debug")
	assert.Equal(t, Result{Outcome: Allowed}, res)

	// Not a configured layer
	assert.Equal(t, Result{Outcome: Allowed}, l.Check(ctx, "other"))
}

func Test_Check_Overrides(t *testing.T) {
	l, _ := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - segment: [ip]
          limit: 1
          period: 60
          overrides:
            - ips: [10.0.0.0/8]
              limit: -1
        - segment: [tenant]
          limit: 2
          calendar: month
          overrides:
            - tenants: [acme]
              limit: 4
            - users: [banned]
              limit: 0
            - anonymous: true
              limit: 1
`+testAuth+`
layers:
  - id: l
    provider: {name: static, color: FFF}
`)

	internal := requestContext("", "acme", "10.1.1.1:1")
	for range 4 {
		res := l.Check(internal, "l")
		require.Equal(t, Allowed, res.Outcome)
		assert.NotContains(t, header(res, HeaderRateLimitPolicy), "a.ip.60s", "unlimited rules aren't reported")
		assert.Contains(t, header(res, HeaderRateLimitPolicy), `"a.tenant.month";q=4`)
	}

	assert.Equal(t, Rejected, l.Check(internal, "l").Outcome)

	res := l.Check(requestContext("banned", "other", "10.1.1.2:1"), "l")
	assert.Equal(t, Rejected, res.Outcome)
	assert.Empty(t, res.Header.Get(HeaderRetryAfter), "retrying never helps against a limit of 0")
	assert.Contains(t, header(res, HeaderRateLimit), `"a.tenant.month";r=0`)

	anonymous := requestContext("", "", "10.1.1.3:1")
	assert.Equal(t, Allowed, l.Check(anonymous, "l").Outcome)
	assert.Equal(t, Rejected, l.Check(anonymous, "l").Outcome)

	external := requestContext("", "globex", "192.0.2.1:1")
	assert.Equal(t, Allowed, l.Check(external, "l").Outcome)
	assert.Equal(t, Rejected, l.Check(external, "l").Outcome)
}

func Test_Check_EveryRuleUnlimited(t *testing.T) {
	l, _ := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - segment: [ip]
          limit: 1
          period: 60
          overrides:
            - ips: [10.0.0.0/8]
              limit: -1
layers:
  - id: l
    provider: {name: static, color: FFF}
`)

	assert.Equal(t, Result{Outcome: Allowed}, l.Check(requestContext("", "", "10.0.0.1:1"), "l"))
}

func Test_Check_HeaderModes(t *testing.T) {
	for mode, wantStandard := range map[string]bool{HeadersMin: false, HeadersNone: false, HeadersStandard: true} {
		l, _ := newTestLimiter(t, `
ratelimit:
  name: memory
  headers: `+mode+`
  policies:
    - id: a
      rules:
        - {segment: [], limit: 1, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)
		ctx := requestContext("", "", "10.0.0.1:1")

		res := l.Check(ctx, "l")
		require.Equal(t, Allowed, res.Outcome)
		assert.Equal(t, wantStandard, header(res, HeaderRateLimit) != "", mode)

		res = l.Check(ctx, "l")
		require.Equal(t, Rejected, res.Outcome)
		assert.Equal(t, mode != HeadersNone, res.Header.Get(HeaderRetryAfter) != "", mode)
		assert.Equal(t, wantStandard, header(res, HeaderRateLimitPolicy) != "", mode)
	}
}

func Test_Check_IPv6Prefix(t *testing.T) {
	l, _ := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [ip], limit: 1, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)

	assert.Equal(t, Allowed, l.Check(requestContext("", "", "[2001:db8:1:2::1]:80"), "l").Outcome)
	assert.Equal(t, Rejected, l.Check(requestContext("", "", "[2001:db8:1:2:ffff::9]:80"), "l").Outcome)
	assert.Equal(t, Allowed, l.Check(requestContext("", "", "[2001:db8:1:3::1]:80"), "l").Outcome)

	// IPv4 is always individual, even mapped into IPv6
	assert.Equal(t, Allowed, l.Check(requestContext("", "", "[::ffff:10.0.0.1]:80"), "l").Outcome)
	assert.Equal(t, Rejected, l.Check(requestContext("", "", "10.0.0.1:80"), "l").Outcome)
	assert.Equal(t, Allowed, l.Check(requestContext("", "", "10.0.0.2:80"), "l").Outcome)

	// Not an address at all still gets a counter
	assert.Equal(t, Allowed, l.Check(requestContext("", "", "garbage"), "l").Outcome)
	assert.Equal(t, Rejected, l.Check(requestContext("", "", "garbage"), "l").Outcome)
}

type failingStore struct {
	getErr error
	incErr error
	counts []int64
	block  bool
}

func (s failingStore) Get(ctx context.Context, counters []ratelimit.RateLimitCounter) ([]int64, error) {
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	if s.counts != nil {
		return s.counts, nil
	}

	return make([]int64, len(counters)), s.getErr
}

func (s failingStore) Increment(_ context.Context, _ []ratelimit.RateLimitCounter) error {
	return s.incErr
}

func Test_Check_StoreFailures(t *testing.T) {
	stores := map[string]failingStore{
		"get":       {getErr: errors.New("down")},
		"increment": {incErr: errors.New("down")},
		"short":     {counts: []int64{}},
		"timeout":   {block: true},
	}

	for name, store := range stores {
		for _, onError := range allOnError {
			l, _ := newTestLimiter(t, `
ratelimit:
  name: memory
  timeout: 5
  onerror: `+onError+`
  policies:
    - id: a
      rules:
        - {segment: [], limit: 1, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`)
			l.store = store

			res := l.Check(requestContext("", "", "10.0.0.1:1"), "l")
			res2 := l.Check(requestContext("", "", "10.0.0.1:1"), "l")

			want := Allowed
			if onError == OnErrorDeny {
				want = Unavailable
			}

			assert.Equal(t, Result{Outcome: want}, res, name)
			assert.Equal(t, Result{Outcome: want}, res2, name)
		}
	}
}

func Test_Check_AuditedOncePerWindow(t *testing.T) {
	buf := captureAudit(t)

	l, clock := newTestLimiter(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [user], limit: 1, period: 1}
`+testAuth+`
layers:
  - id: l
    provider: {name: static, color: FFF}
`)
	ctx := requestContext("alice", "acme", "10.0.0.1:1")

	require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)

	for range 5 {
		require.Equal(t, Rejected, l.Check(ctx, "l").Outcome)
	}

	assert.Equal(t, 1, strings.Count(buf.String(), audit.EventRateLimited))
	assert.Contains(t, buf.String(), `"rule":"a.user.1s"`)
	assert.Contains(t, buf.String(), `"layer":"l"`)
	assert.Contains(t, buf.String(), `"user":"alice"`)

	// Short periods are only reported once a minute
	clock.advance(30 * time.Second)
	require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
	require.Equal(t, Rejected, l.Check(ctx, "l").Outcome)
	assert.Equal(t, 1, strings.Count(buf.String(), audit.EventRateLimited))

	clock.advance(time.Minute)
	require.Equal(t, Allowed, l.Check(ctx, "l").Outcome)
	require.Equal(t, Rejected, l.Check(ctx, "l").Outcome)
	assert.Equal(t, 2, strings.Count(buf.String(), audit.EventRateLimited))
}

func Test_Limiter_NilSafe(t *testing.T) {
	var l *Limiter

	assert.False(t, l.Enabled())
	require.NoError(t, l.Close(context.Background()))
	assert.Equal(t, Result{Outcome: Allowed}, l.Check(context.Background(), "l"))
	assert.Equal(t, []string{HeaderRateLimit, HeaderRateLimitPolicy, HeaderRetryAfter}, ExposedHeaders())
}

func Test_None_CountsNothing(t *testing.T) {
	s, err := NoneRegistration{}.Initialize(NoneRegistration{}.InitializeConfig(), ratelimit.RateLimiterDeps{})
	require.NoError(t, err)

	counts, err := s.Get(context.Background(), make([]ratelimit.RateLimitCounter, 2))
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 0}, counts)
	require.NoError(t, s.Increment(context.Background(), nil))
}
