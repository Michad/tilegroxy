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
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
)

const (
	SegmentUser   = "user"
	SegmentTenant = "tenant"
	SegmentIP     = "ip"
	SegmentLayer  = "layer"
)

// Also the order segments appear in rule names, so reordering a config's segment list doesn't reset counters
var allSegments = []string{SegmentUser, SegmentTenant, SegmentIP, SegmentLayer}

const (
	CalendarHour  = "hour"
	CalendarDay   = "day"
	CalendarWeek  = "week"
	CalendarMonth = "month"
	CalendarYear  = "year"
)

var allCalendars = []string{CalendarHour, CalendarDay, CalendarWeek, CalendarMonth, CalendarYear}

// Override limits with special meaning
const (
	limitUnlimited = -1
	limitReject    = 0
)

// The leading bits of an IPv4-mapped IPv6 address that only mark the mapping
const ipv4MappedBits = 96

// Policy IDs end up in rule names, store keys, metric names, and headers
var policyIDRegex = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type PolicyConfig struct {
	ID     string
	Rules  []RuleConfig
	Extend []string
}

type RuleConfig struct {
	Limit    uint
	Period   uint
	Calendar string
	// A pointer because an empty list is valid and differs from a missing one
	Segment   *[]string
	Overrides []OverrideConfig
}

type OverrideConfig struct {
	Users     []string
	Tenants   []string
	IPs       []string
	Anonymous bool
	// A pointer since 0 is meaningful and the key is required
	Limit *int
}

type rule struct {
	policy   string
	name     string
	segments []string
	// Zero for a calendar rule
	period    time.Duration
	calendar  string
	limit     int64
	overrides []override
}

type override struct {
	users     map[string]bool
	tenants   map[string]bool
	ips       []netip.Prefix
	anonymous bool
	limit     int64
}

type policy struct {
	id    string
	rules []*rule
	// Own rules plus those of every policy it extends, each once, extended ones first
	all []*rule
}

func (r *rule) hasSegment(segment string) bool {
	return slices.Contains(r.segments, segment)
}

// The override limit, or the rule's own when none match
func (r *rule) limitFor(id identity) int64 {
	for _, o := range r.overrides {
		if o.matches(r, id) {
			return o.limit
		}
	}

	return r.limit
}

func (o override) matches(r *rule, id identity) bool {
	if id.user != "" && o.users[id.user] {
		return true
	}

	if id.tenant != "" && o.tenants[id.tenant] {
		return true
	}

	if id.addr.IsValid() {
		for _, p := range o.ips {
			if p.Contains(id.addr) {
				return true
			}
		}
	}

	if o.anonymous {
		if r.hasSegment(SegmentUser) && id.user == "" {
			return true
		}

		if r.hasSegment(SegmentTenant) && id.tenant == "" {
			return true
		}
	}

	return false
}

func ruleName(policyID string, segments []string, period time.Duration, calendar string) string {
	segment := "all"
	if len(segments) > 0 {
		segment = strings.Join(segments, "+")
	}

	window := calendar
	if period > 0 {
		window = strconv.FormatInt(int64(period/time.Second), 10) + "s"
	}

	return policyID + "." + segment + "." + window
}

// Compiles and validates every policy. authEnabled is false when authentication is none
func compilePolicies(cfgs []PolicyConfig, authEnabled bool, messages config.ErrorMessages) ([]*policy, error) {
	policies := make([]*policy, 0, len(cfgs))
	byID := make(map[string]*policy, len(cfgs))

	for i, pc := range cfgs {
		param := fmt.Sprintf("ratelimit.policies[%d]", i)

		if pc.ID == "" {
			return nil, fmt.Errorf(messages.ParamRequired, param+".id")
		}

		if !policyIDRegex.MatchString(pc.ID) {
			return nil, fmt.Errorf(messages.ParamRegex, param+".id", pc.ID, policyIDRegex.String())
		}

		if byID[pc.ID] != nil {
			return nil, fmt.Errorf(messages.MustBeUnique, param+".id", pc.ID)
		}

		p := &policy{id: pc.ID}

		for j, rc := range pc.Rules {
			r, err := compileRule(pc.ID, rc, fmt.Sprintf("%s.rules[%d]", param, j), authEnabled, messages)
			if err != nil {
				return nil, err
			}

			if slices.ContainsFunc(p.rules, func(other *rule) bool { return other.name == r.name }) {
				return nil, fmt.Errorf(messages.MustBeUnique, fmt.Sprintf("%s.rules[%d]", param, j), r.name)
			}

			p.rules = append(p.rules, r)
		}

		policies = append(policies, p)
		byID[p.id] = p
	}

	for i, pc := range cfgs {
		for _, ext := range pc.Extend {
			if byID[ext] == nil {
				return nil, fmt.Errorf(messages.InvalidParam, fmt.Sprintf("ratelimit.policies[%d].extend", i), ext)
			}
		}
	}

	extends := make(map[string][]string, len(cfgs))
	for _, pc := range cfgs {
		extends[pc.ID] = pc.Extend
	}

	for i, p := range policies {
		all, err := resolveExtends(p.id, extends, byID, nil)
		if err != nil {
			return nil, fmt.Errorf(messages.InvalidParam, fmt.Sprintf("ratelimit.policies[%d].extend", i), err.Error())
		}

		p.all = all
	}

	return policies, nil
}

// Depth first so extended rules come before the policy's own. The path detects loops
func resolveExtends(id string, extends map[string][]string, byID map[string]*policy, path []string) ([]*rule, error) {
	if slices.Contains(path, id) {
		return nil, fmt.Errorf("extend loop %v", strings.Join(append(path, id), " -> "))
	}

	path = append(path, id)

	var all []*rule

	for _, ext := range extends[id] {
		extRules, err := resolveExtends(ext, extends, byID, path)
		if err != nil {
			return nil, err
		}

		for _, r := range extRules {
			if !slices.Contains(all, r) {
				all = append(all, r)
			}
		}
	}

	for _, r := range byID[id].rules {
		if !slices.Contains(all, r) {
			all = append(all, r)
		}
	}

	return all, nil
}

func compileRule(policyID string, rc RuleConfig, param string, authEnabled bool, messages config.ErrorMessages) (*rule, error) {
	if rc.Limit < 1 {
		return nil, fmt.Errorf(messages.InvalidParam, param+".limit", rc.Limit)
	}

	r := &rule{policy: policyID, limit: int64(rc.Limit)} // #nosec G115 -- operator-supplied limit, far below int64 overflow range

	switch {
	case rc.Period > 0 && rc.Calendar != "":
		return nil, fmt.Errorf(messages.ParamsMutuallyExclusive, param+".period", param+".calendar")
	case rc.Period > 0:
		r.period = time.Duration(rc.Period) * time.Second // #nosec G115 -- operator-supplied period in seconds, far below int64 overflow range
	case rc.Calendar != "":
		r.calendar = strings.ToLower(rc.Calendar)

		if !slices.Contains(allCalendars, r.calendar) {
			return nil, fmt.Errorf(messages.EnumError, param+".calendar", rc.Calendar, allCalendars)
		}
	default:
		return nil, fmt.Errorf(messages.OneOfRequired, []string{param + ".period", param + ".calendar"})
	}

	segments, err := compileSegments(rc.Segment, param+".segment", authEnabled, messages)
	if err != nil {
		return nil, err
	}

	r.segments = segments

	for i, oc := range rc.Overrides {
		o, err := compileOverride(oc, fmt.Sprintf("%s.overrides[%d]", param, i), messages)
		if err != nil {
			return nil, err
		}

		r.overrides = append(r.overrides, o)
	}

	r.name = ruleName(policyID, r.segments, r.period, r.calendar)

	return r, nil
}

// Returned in the canonical order of allSegments
func compileSegments(raw *[]string, param string, authEnabled bool, messages config.ErrorMessages) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf(messages.ParamRequired, param)
	}

	seen := make(map[string]bool, len(*raw))

	for _, s := range *raw {
		s = strings.ToLower(s)

		if !slices.Contains(allSegments, s) {
			return nil, fmt.Errorf(messages.EnumError, param, s, allSegments)
		}

		if seen[s] {
			return nil, fmt.Errorf(messages.MustBeUnique, param, s)
		}

		if !authEnabled && (s == SegmentUser || s == SegmentTenant) {
			return nil, fmt.Errorf(messages.ParamRequiresParam, param+"="+s, "authentication")
		}

		seen[s] = true
	}

	segments := make([]string, 0, len(seen))
	for _, s := range allSegments {
		if seen[s] {
			segments = append(segments, s)
		}
	}

	return segments, nil
}

func compileOverride(oc OverrideConfig, param string, messages config.ErrorMessages) (override, error) {
	if len(oc.Users) == 0 && len(oc.Tenants) == 0 && len(oc.IPs) == 0 && !oc.Anonymous {
		return override{}, fmt.Errorf(messages.OneOfRequired, []string{param + ".users", param + ".tenants", param + ".ips", param + ".anonymous"})
	}

	if oc.Limit == nil {
		return override{}, fmt.Errorf(messages.ParamRequired, param+".limit")
	}

	if *oc.Limit < limitUnlimited {
		return override{}, fmt.Errorf(messages.InvalidParam, param+".limit", *oc.Limit)
	}

	o := override{
		users:     toSet(oc.Users),
		tenants:   toSet(oc.Tenants),
		anonymous: oc.Anonymous,
		limit:     int64(*oc.Limit),
	}

	for _, raw := range oc.IPs {
		prefix, err := ParsePrefix(raw)
		if err != nil {
			return override{}, fmt.Errorf(messages.InvalidParam, param+".ips", raw)
		}

		o.ips = append(o.ips, prefix)
	}

	return o, nil
}

// Accepts a single address or a CIDR range
func ParsePrefix(raw string) (netip.Prefix, error) {
	if strings.Contains(raw, "/") {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return netip.Prefix{}, err
		}

		if prefix.Addr().Is4In6() {
			return netip.PrefixFrom(prefix.Addr().Unmap(), max(prefix.Bits()-ipv4MappedBits, 0)).Masked(), nil
		}

		return prefix.Masked(), nil
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, err
	}

	addr = addr.Unmap().WithZone("")

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}

	return set
}
