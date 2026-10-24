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

// Package ratelimit enforces rate limit policies on tile requests and holds the built-in counter stores
package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/internal/static"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	HeadersStandard = "standard"
	HeadersMin      = "min"
	HeadersNone     = "none"
)

var allHeaders = []string{HeadersStandard, HeadersMin, HeadersNone}

const (
	OnErrorAllow = "allow"
	OnErrorDeny  = "deny"
)

var allOnError = []string{OnErrorAllow, OnErrorDeny}

const (
	HeaderRateLimit       = "RateLimit"
	HeaderRateLimitPolicy = "RateLimit-Policy"
	HeaderRetryAfter      = "Retry-After"
)

// The hex characters of a hashed segment value kept in a key. Half a SHA-256, plenty to avoid collisions
const keyHashLen = 32

// The longest a repeated rejection or store failure goes unreported
const reportInterval = time.Minute

// Bounds the memory spent remembering which rejections were already audited
const auditMemory = 10000

// Leaves room for the metric prefix and suffix within OTEL's 255 character limit
const maxMetricPolicyLen = 200

var packageName = static.GetPackage()

type Outcome int

const (
	Allowed Outcome = iota
	Rejected
	// The store failed and onError is deny
	Unavailable
)

// What to do with one request. Header holds the headers to add to the response either way
type Result struct {
	Outcome Outcome
	Header  http.Header
	// The first exceeded rule, for logging
	Rule string
}

// Who a request is counted as
type identity struct {
	user   string
	tenant string
	// Masked to the IPv6 prefix. The raw value when it isn't an address
	ip string
	// Invalid when the client IP isn't an address
	addr  netip.Addr
	layer string
}

// One rule as it applies to one request
type evaluation struct {
	rule    *rule
	limit   int64
	window  window
	key     string
	cur     int64
	prev    int64
	used    int64
	counted bool
}

// Enforces the policies of one configuration
type Limiter struct {
	enabled       bool
	store         ratelimit.RateLimiter
	storeName     string
	policies      []*policy
	byID          map[string]*policy
	defaultPolicy *policy
	// Policy by layer ID. Absent for layers that skip rate limiting
	layerPolicies map[string]*policy
	headers       string
	onError       string
	timeout       time.Duration
	ipv6Prefix    int
	now           func() time.Time
	clock         ratelimit.Clock
	// Nanoseconds to add to now to get the store's time
	clockOffset atomic.Int64
	lastSync    atomic.Int64

	tracer          trace.Tracer
	rejectedCounter metric.Int64Counter
	errorCounter    metric.Int64Counter
	policyCounters  map[string]metric.Int64Counter

	audited  *lru
	lastWarn atomic.Int64
}

// False for the none store, in which case Check always allows without headers
func (l *Limiter) Enabled() bool {
	return l != nil && l.enabled
}

// The headers to let browser scripts read when rate limiting is enabled
func ExposedHeaders() []string {
	return []string{HeaderRateLimit, HeaderRateLimitPolicy, HeaderRetryAfter}
}

func (l *Limiter) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}

	return lifecycle.CloseIfCloser(ctx, l.store)
}

func (l *Limiter) initTelemetry() error {
	meter := otel.Meter(packageName)

	errs := make([]error, 0, len(l.policies)+2)
	var err error

	l.tracer = otel.Tracer(packageName)

	l.rejectedCounter, err = meter.Int64Counter("tilegroxy.ratelimit.total.rejected", metric.WithDescription("Number of tile requests rejected by a rate limit"))
	errs = append(errs, err)

	l.errorCounter, err = meter.Int64Counter("tilegroxy.ratelimit.error", metric.WithDescription("Number of rate limit checks that failed because the store failed or timed out"))
	errs = append(errs, err)

	l.policyCounters = make(map[string]metric.Int64Counter, len(l.policies))

	for _, p := range l.policies {
		name := p.id
		if len(name) > maxMetricPolicyLen {
			name = name[:maxMetricPolicyLen]
		}

		l.policyCounters[p.id], err = meter.Int64Counter("tilegroxy.ratelimit.policy."+name+".rejected", metric.WithDescription("Number of tile requests rejected by a rule of rate limit policy "+p.id))
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (l *Limiter) identify(user, tenant, ip, layer string) identity {
	id := identity{user: user, tenant: tenant, ip: ip, layer: layer}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return id
	}

	addr = addr.Unmap().WithZone("")
	id.addr = addr

	if addr.Is6() {
		prefix, _ := addr.Prefix(l.ipv6Prefix)
		id.ip = prefix.Addr().String()
	} else {
		id.ip = addr.String()
	}

	return id
}

func identityFromContext(ctx context.Context) (string, string, string) {
	var user, tenant string

	if u, ok := pkg.UserIDFromContext(ctx); ok && u != nil {
		user = *u
	}

	if t, ok := pkg.TenantIDFromContext(ctx); ok && t != nil {
		tenant = *t
	}

	ip, _ := ctx.Value("ip").(string)

	return user, tenant, ip
}

// Hashed so arbitrary IDs are safe in any store, length-prefixed so values can't run together
func (r *rule) key(id identity) string {
	h := sha256.New()

	for _, s := range r.segments {
		var v string

		switch s {
		case SegmentUser:
			v = id.user
		case SegmentTenant:
			v = id.tenant
		case SegmentIP:
			v = id.ip
		case SegmentLayer:
			v = id.layer
		}

		_ = binary.Write(h, binary.BigEndian, uint64(len(v)))
		_, _ = h.Write([]byte(v))
	}

	return r.name + ":" + hex.EncodeToString(h.Sum(nil))[:keyHashLen]
}

// Unlimited rules are left out since they're neither checked nor counted
func (l *Limiter) evaluate(p *policy, id identity, now time.Time) []*evaluation {
	evals := make([]*evaluation, 0, len(p.all))

	for _, r := range p.all {
		limit := r.limitFor(id)
		if limit == limitUnlimited {
			continue
		}

		evals = append(evals, &evaluation{rule: r, limit: limit, window: r.windowAt(now), key: r.key(id)})
	}

	return evals
}

func counters(evals []*evaluation, withPrevious bool) []ratelimit.RateLimitCounter {
	result := make([]ratelimit.RateLimitCounter, 0, 2*len(evals))

	for _, e := range evals {
		result = append(result, ratelimit.RateLimitCounter{Key: e.key, Window: e.window.start, TTL: e.window.ttl})

		if withPrevious && e.window.rolling() {
			result = append(result, ratelimit.RateLimitCounter{Key: e.key, Window: e.window.prevStart, TTL: e.window.ttl})
		}
	}

	return result
}

// Fills in each evaluation's counts from the store
func (l *Limiter) load(ctx context.Context, evals []*evaluation) error {
	counts, err := l.store.Get(ctx, counters(evals, true))
	if err != nil {
		return err
	}

	i := 0
	for _, e := range evals {
		if i >= len(counts) {
			return fmt.Errorf("rate limit store %v returned %v counts for more counters", l.storeName, len(counts))
		}

		e.cur = counts[i]
		i++

		if e.window.rolling() {
			if i >= len(counts) {
				return fmt.Errorf("rate limit store %v returned %v counts for more counters", l.storeName, len(counts))
			}

			e.prev = counts[i]
			i++
		}

		e.used = e.window.used(e.cur, e.prev)
	}

	return nil
}

// Nil when the layer isn't rate limited
func (l *Limiter) policyForLayer(layerID string) *policy {
	if !l.Enabled() {
		return nil
	}

	return l.layerPolicies[layerID]
}

// Checks and counts a tile request for the layer. The identity comes from the request context
func (l *Limiter) Check(ctx context.Context, layerID string) Result {
	p := l.policyForLayer(layerID)
	if p == nil || len(p.all) == 0 {
		return Result{Outcome: Allowed}
	}

	ctx, span := l.tracer.Start(ctx, "ratelimit", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	if span.IsRecording() {
		span.SetAttributes(attribute.String("tilegroxy.ratelimit.policy", p.id))
	}

	user, tenant, ip := identityFromContext(ctx)
	id := l.identify(user, tenant, ip, layerID)
	now := l.windowNow()
	evals := l.evaluate(p, id, now)

	if len(evals) == 0 {
		return Result{Outcome: Allowed}
	}

	storeCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	if err := l.load(storeCtx, evals); err != nil {
		return l.failed(ctx, span, err)
	}

	var exceeded []*evaluation

	for _, e := range evals {
		if e.used >= e.limit {
			exceeded = append(exceeded, e)
		}
	}

	if len(exceeded) > 0 {
		span.SetStatus(codes.Error, "Rate limited")
		l.reject(ctx, exceeded, id, layerID)

		return Result{Outcome: Rejected, Header: l.buildHeaders(evals, exceeded, now), Rule: exceeded[0].rule.name}
	}

	if err := l.store.Increment(storeCtx, counters(evals, false)); err != nil {
		return l.failed(ctx, span, err)
	}

	for _, e := range evals {
		e.counted = true
	}

	return Result{Outcome: Allowed, Header: l.buildHeaders(evals, nil, now)}
}

func (l *Limiter) failed(ctx context.Context, span trace.Span, err error) Result {
	span.RecordError(err)
	span.SetStatus(codes.Error, "Rate limit store error")

	l.errorCounter.Add(ctx, 1)

	now := l.now().UnixNano()
	last := l.lastWarn.Load()

	if now-last >= int64(reportInterval) && l.lastWarn.CompareAndSwap(last, now) {
		slog.WarnContext(ctx, fmt.Sprintf("Rate limit store %v failed, so requests are being handled per onError=%v: %v", l.storeName, l.onError, err))
	}

	if l.onError == OnErrorDeny {
		return Result{Outcome: Unavailable}
	}

	return Result{Outcome: Allowed}
}

func (l *Limiter) reject(ctx context.Context, exceeded []*evaluation, id identity, layerID string) {
	l.rejectedCounter.Add(ctx, 1)

	counted := map[string]bool{}

	for _, e := range exceeded {
		if !counted[e.rule.policy] {
			counted[e.rule.policy] = true
			l.policyCounters[e.rule.policy].Add(ctx, 1)
		}

		if !audit.Enabled() {
			continue
		}

		// Once per window, or per minute for shorter periods
		interval := max(e.window.end.Sub(e.window.start), reportInterval)
		slot := e.window.start.UnixNano() / int64(interval)
		auditKey := e.key + "@" + strconv.FormatInt(slot, 10)

		if l.audited.add(auditKey, l.now(), interval) == 1 {
			audit.RateLimited(ctx, e.rule.policy, e.rule.name, layerID, id.user, id.tenant)
		}
	}
}

func (l *Limiter) buildHeaders(evals []*evaluation, exceeded []*evaluation, now time.Time) http.Header {
	header := http.Header{}

	if l.headers == HeadersNone {
		return header
	}

	if len(exceeded) > 0 {
		if retry, ok := retryAfter(exceeded, now); ok {
			header.Set(HeaderRetryAfter, strconv.FormatInt(retry, 10))
		}
	}

	if l.headers != HeadersStandard {
		return header
	}

	policies := make([]string, 0, len(evals))
	states := make([]string, 0, len(evals))

	for _, e := range evals {
		used := e.used
		if e.counted {
			used++
		}

		policies = append(policies, fmt.Sprintf("%q;q=%d;w=%d", e.rule.name, e.limit, ceilSeconds(e.window.end.Sub(e.window.start))))
		states = append(states, fmt.Sprintf("%q;r=%d;t=%d", e.rule.name, max(e.limit-used, 0), ceilSeconds(e.window.end.Sub(now))))
	}

	// Assigned directly since Set would canonicalize away the spelling the draft standard uses
	header[HeaderRateLimitPolicy] = []string{strings.Join(policies, ", ")}
	header[HeaderRateLimit] = []string{strings.Join(states, ", ")}

	return header
}

// Seconds until every exceeded rule allows another request. False when a limit of 0 means none ever will
func retryAfter(exceeded []*evaluation, now time.Time) (int64, bool) {
	var longest time.Duration

	for _, e := range exceeded {
		if e.limit == limitReject {
			return 0, false
		}

		longest = max(longest, e.window.retryAfter(now, e.cur, e.prev, e.limit))
	}

	return ceilSeconds(longest), true
}
