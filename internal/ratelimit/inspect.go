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
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
)

var ErrNotInspectable = errors.New("the rate limit store can't be read or reset from outside the server")

// Stands in for the values a tile request would have. Empty IP or Layer means not supplied
type InspectOptions struct {
	Policies []string
	Layer    string
	User     string
	Tenant   string
	IP       string
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
}

// Rules are skipped, with a warning, when they need an IP or layer that wasn't supplied
func (l *Limiter) selectRules(opts InspectOptions) ([]*rule, []string, error) {
	var selected []*policy

	switch {
	case len(opts.Policies) > 0:
		for _, id := range opts.Policies {
			p := l.byID[id]
			if p == nil {
				return nil, nil, fmt.Errorf("unknown rate limit policy %q", id)
			}

			selected = append(selected, p)
		}
	case opts.Layer != "":
		p, ok := l.layerPolicies[opts.Layer]
		if !ok {
			return nil, nil, fmt.Errorf("layer %q doesn't exist or isn't rate limited", opts.Layer)
		}

		selected = append(selected, p)
	default:
		selected = l.policies
	}

	var rules []*rule
	var warnings []string

	for _, p := range selected {
		for _, r := range p.all {
			if slices.Contains(rules, r) {
				continue
			}

			if r.hasSegment(SegmentIP) && opts.IP == "" {
				warnings = append(warnings, "Skipping "+r.name+" since it needs --ip")
				continue
			}

			if r.hasSegment(SegmentLayer) && opts.Layer == "" {
				warnings = append(warnings, "Skipping "+r.name+" since it needs --layer")
				continue
			}

			rules = append(rules, r)
		}
	}

	return rules, warnings, nil
}

func (l *Limiter) resetter() (ratelimit.RateLimitResetter, error) {
	resetter, ok := l.store.(ratelimit.RateLimitResetter)
	if !ok || !l.Enabled() {
		return nil, fmt.Errorf("%w: %v", ErrNotInspectable, l.storeName)
	}

	return resetter, nil
}

// The usage of every matching rule, along with warnings about rules skipped
func (l *Limiter) Inspect(ctx context.Context, opts InspectOptions) ([]Usage, []string, error) {
	if _, err := l.resetter(); err != nil {
		return nil, nil, err
	}

	rules, warnings, err := l.selectRules(opts)
	if err != nil || len(rules) == 0 {
		return nil, warnings, err
	}

	id := l.identify(opts.User, opts.Tenant, opts.IP, opts.Layer)
	now := l.windowNow()

	evals := make([]*evaluation, 0, len(rules))
	for _, r := range rules {
		evals = append(evals, &evaluation{rule: r, limit: r.limitFor(id), window: r.windowAt(now), key: r.key(id)})
	}

	if err := l.load(ctx, evals); err != nil {
		return nil, warnings, err
	}

	usages := make([]Usage, 0, len(evals))

	for _, e := range evals {
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
		}

		usages = append(usages, u)
	}

	return usages, warnings, nil
}

// Sets the matching counters back to zero, including the previous bucket of a rolling period. dryRun only reports them
func (l *Limiter) Reset(ctx context.Context, opts InspectOptions, dryRun bool) ([]string, []string, error) {
	resetter, err := l.resetter()
	if err != nil {
		return nil, nil, err
	}

	rules, warnings, err := l.selectRules(opts)
	if err != nil || len(rules) == 0 {
		return nil, warnings, err
	}

	id := l.identify(opts.User, opts.Tenant, opts.IP, opts.Layer)
	now := l.windowNow()

	evals := make([]*evaluation, 0, len(rules))
	names := make([]string, 0, len(rules))

	for _, r := range rules {
		evals = append(evals, &evaluation{rule: r, window: r.windowAt(now), key: r.key(id)})
		names = append(names, r.name)
	}

	if dryRun {
		return names, warnings, nil
	}

	if err := resetter.Reset(ctx, counters(evals, true)); err != nil {
		return nil, warnings, err
	}

	return names, warnings, nil
}
