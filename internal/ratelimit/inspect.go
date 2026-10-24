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
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
)

var ErrNotInspectable = errors.New("the rate limit store can't be read or reset from outside the server")

// Matches rules with no segments, the same word used in their names
const SegmentAll = "all"

// Stands in for the identity a tile request would have. An empty IP means not supplied
type Request struct {
	User   string
	Tenant string
	IP     string
}

// One rule's state for the given identity
type Usage struct {
	Rule      string    `json:"rule"`
	Policy    string    `json:"policy"`
	Used      int64     `json:"used"`
	Limit     int64     `json:"limit"`
	Unlimited bool      `json:"unlimited"`
	Remaining int64     `json:"remaining"`
	Resets    time.Time `json:"resets"`
	// False for a calendar period
	Rolling bool `json:"rolling"`
	// True when the rule would reject the next request
	Blocking bool `json:"blocking"`
}

// Whether the next tile request to one layer would be allowed
type Status struct {
	Layer string `json:"layer"`
	// Empty when the layer isn't rate limited
	Policy  string `json:"policy,omitempty"`
	Allowed bool   `json:"allowed"`
	// In seconds. Absent when allowed, or when a limit of 0 means it never will be
	RetryAfter *int64  `json:"retryAfter,omitempty"`
	Rules      []Usage `json:"rules"`
}

// Each field narrows the result. A rule matches a field if it matches any of its values
type RuleFilter struct {
	// Case-insensitive substring of the rule name
	Search   string
	Policies []string
	// Layer IDs
	Layers   []string
	Segments []string
}

type RuleInfo struct {
	Rule     string   `json:"rule"`
	Policy   string   `json:"policy"`
	Segments []string `json:"segments"`
	// In seconds. Zero for a calendar period
	Period    int64          `json:"period,omitempty"`
	Calendar  string         `json:"calendar,omitempty"`
	Limit     int64          `json:"limit"`
	Overrides []OverrideInfo `json:"overrides"`
	// Layers enforcing the rule, directly or through extend
	Layers []string `json:"layers"`
}

type OverrideInfo struct {
	Users     []string `json:"users,omitempty"`
	Tenants   []string `json:"tenants,omitempty"`
	IPs       []string `json:"ips,omitempty"`
	Anonymous bool     `json:"anonymous,omitempty"`
	Limit     int64    `json:"limit"`
}

func (l *Limiter) resetter() (ratelimit.RateLimitResetter, error) {
	resetter, ok := l.store.(ratelimit.RateLimitResetter)
	if !ok || !l.Enabled() {
		return nil, fmt.Errorf("%w: %v", ErrNotInspectable, l.storeName)
	}

	return resetter, nil
}

func (e *evaluation) usage() Usage {
	u := Usage{
		Rule:      e.rule.name,
		Policy:    e.rule.policy,
		Used:      e.used,
		Limit:     e.limit,
		Unlimited: e.limit == limitUnlimited,
		Remaining: max(e.limit-e.used, 0),
		Resets:    e.window.end,
		Rolling:   e.window.rolling(),
	}

	if u.Unlimited {
		u.Remaining = 0
	} else {
		u.Blocking = e.used >= e.limit
	}

	return u
}

// Reads the counters without counting a request. Rules needing an IP are skipped, with a warning, when none is supplied
func (l *Limiter) Status(ctx context.Context, layerID string, req Request) (Status, []string, error) {
	if _, err := l.resetter(); err != nil {
		return Status{}, nil, err
	}

	status := Status{Layer: layerID, Allowed: true, Rules: []Usage{}}

	p := l.layerPolicies[layerID]
	if p == nil {
		return status, nil, nil
	}

	status.Policy = p.id

	id := l.identify(req.User, req.Tenant, req.IP, layerID)
	now := l.windowNow()

	var warnings []string
	evals := make([]*evaluation, 0, len(p.all))

	for _, r := range p.all {
		if r.hasSegment(SegmentIP) && req.IP == "" {
			warnings = append(warnings, "Skipping "+r.name+" since it needs --ip")
			continue
		}

		evals = append(evals, &evaluation{rule: r, limit: r.limitFor(id), window: r.windowAt(now), key: r.key(id)})
	}

	if len(evals) == 0 {
		return status, warnings, nil
	}

	if err := l.load(ctx, evals); err != nil {
		return Status{}, warnings, err
	}

	var exceeded []*evaluation

	for _, e := range evals {
		u := e.usage()
		if u.Blocking {
			exceeded = append(exceeded, e)
		}

		status.Rules = append(status.Rules, u)
	}

	if len(exceeded) > 0 {
		status.Allowed = false

		if retry, ok := retryAfter(exceeded, now); ok {
			status.RetryAfter = &retry
		}
	}

	return status, warnings, nil
}

func (l *Limiter) findRule(name string) *rule {
	for _, p := range l.policies {
		for _, r := range p.rules {
			if r.name == name {
				return r
			}
		}
	}

	return nil
}

// Clears one counter, including the previous bucket of a rolling period. Returns the count beforehand.
// segments must hold a value, possibly empty, for exactly the segments of the rule
func (l *Limiter) ResetRule(ctx context.Context, name string, segments map[string]string, dryRun bool) (int64, []string, error) {
	resetter, err := l.resetter()
	if err != nil {
		return 0, nil, err
	}

	r := l.findRule(name)
	if r == nil {
		return 0, nil, fmt.Errorf("unknown rate limit rule %q", name)
	}

	for _, s := range allSegments {
		_, given := segments[s]

		if r.hasSegment(s) && !given {
			return 0, nil, fmt.Errorf("rule %v is segmented by %v, so --%v is required", r.name, s, s)
		}

		if !r.hasSegment(s) && given {
			return 0, nil, fmt.Errorf("rule %v isn't segmented by %v, so --%v can't be used", r.name, s, s)
		}
	}

	var warnings []string

	if layer, ok := segments[SegmentLayer]; ok {
		if p := l.layerPolicies[layer]; p == nil || !slices.Contains(p.all, r) {
			warnings = append(warnings, fmt.Sprintf("Layer %q isn't limited by %v", layer, r.name))
		}
	}

	id := l.identify(segments[SegmentUser], segments[SegmentTenant], segments[SegmentIP], segments[SegmentLayer])
	evals := []*evaluation{{rule: r, window: r.windowAt(l.windowNow()), key: r.key(id)}}

	if err := l.load(ctx, evals); err != nil {
		return 0, warnings, err
	}

	if !dryRun {
		if err := resetter.Reset(ctx, counters(evals, true)); err != nil {
			return 0, warnings, err
		}
	}

	return evals[0].used, warnings, nil
}

// Every rule matching the filter, in configuration order. Never touches the store
func (l *Limiter) Rules(filter RuleFilter) ([]RuleInfo, error) {
	for _, id := range filter.Policies {
		if l.byID[id] == nil {
			return nil, fmt.Errorf("unknown rate limit policy %q", id)
		}
	}

	segments := make([]string, 0, len(filter.Segments))

	for _, s := range filter.Segments {
		s = strings.ToLower(s)
		if s != SegmentAll && !slices.Contains(allSegments, s) {
			return nil, fmt.Errorf("unknown segment %q, must be one of %v or %v", s, allSegments, SegmentAll)
		}

		segments = append(segments, s)
	}

	filter.Segments = segments

	result := []RuleInfo{}

	for _, p := range l.policies {
		for _, r := range p.rules {
			if l.ruleMatches(r, filter) {
				result = append(result, l.describe(r))
			}
		}
	}

	return result, nil
}

func (l *Limiter) ruleMatches(r *rule, filter RuleFilter) bool {
	if !strings.Contains(strings.ToLower(r.name), strings.ToLower(filter.Search)) {
		return false
	}

	if len(filter.Policies) > 0 && !slices.ContainsFunc(filter.Policies, func(id string) bool { return slices.Contains(l.byID[id].all, r) }) {
		return false
	}

	if len(filter.Layers) > 0 && !slices.ContainsFunc(filter.Layers, func(id string) bool { return l.layerPolicies[id] != nil && slices.Contains(l.layerPolicies[id].all, r) }) {
		return false
	}

	if len(filter.Segments) > 0 && !slices.ContainsFunc(filter.Segments, func(s string) bool { return r.hasSegment(s) || (s == SegmentAll && len(r.segments) == 0) }) {
		return false
	}

	return true
}

func (l *Limiter) describe(r *rule) RuleInfo {
	info := RuleInfo{
		Rule:      r.name,
		Policy:    r.policy,
		Segments:  append([]string{}, r.segments...),
		Period:    int64(r.period / time.Second),
		Calendar:  r.calendar,
		Limit:     r.limit,
		Overrides: make([]OverrideInfo, 0, len(r.overrides)),
		Layers:    []string{},
	}

	for _, o := range r.overrides {
		oi := OverrideInfo{Anonymous: o.anonymous, Limit: o.limit}

		for u := range o.users {
			oi.Users = append(oi.Users, u)
		}

		for t := range o.tenants {
			oi.Tenants = append(oi.Tenants, t)
		}

		for _, prefix := range o.ips {
			oi.IPs = append(oi.IPs, prefix.String())
		}

		slices.Sort(oi.Users)
		slices.Sort(oi.Tenants)

		info.Overrides = append(info.Overrides, oi)
	}

	for _, id := range l.layerIDs {
		if slices.Contains(l.layerPolicies[id].all, r) {
			info.Layers = append(info.Layers, id)
		}
	}

	return info
}
