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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/Michad/tilegroxy/internal/secrets"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	pkgratelimit "github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

type RateLimitStatusOptions struct {
	ratelimit.Request
	// As they'd appear in a tile request URL
	LayerNames []string
	// Include rules that aren't blocking
	All  bool
	JSON bool
}

type RateLimitResetOptions struct {
	Rule string
	// A value, possibly empty, for each segment of the rule
	Segments map[string]string
	DryRun   bool
	JSON     bool
}

type RateLimitListOptions struct {
	ratelimit.RuleFilter
	JSON bool
}

const tablePadding = 4

// The status along with the layer name it was requested as, which differs from the ID for a pattern
type layerStatus struct {
	Name string `json:"name"`
	ratelimit.Status
}

func RateLimitStatus(cfg *config.Config, opts RateLimitStatusOptions, out io.Writer, errOut io.Writer) error {
	if len(opts.LayerNames) == 0 {
		return errors.New("at least one --layer is required")
	}

	layerIDs := make([]string, 0, len(opts.LayerNames))

	for _, name := range opts.LayerNames {
		id, ok := layerIDForName(cfg, name)
		if !ok {
			return fmt.Errorf("invalid layer name: %v", name)
		}

		layerIDs = append(layerIDs, id)
	}

	return withLimiter(cfg, func(ctx context.Context, limiter *ratelimit.Limiter) error {
		statuses := make([]layerStatus, 0, len(layerIDs))
		var warnings []string

		for i, id := range layerIDs {
			status, w, err := limiter.Status(ctx, id, opts.Request)
			warnings = appendNew(warnings, w...)

			if err != nil {
				writeWarnings(errOut, warnings)
				return err
			}

			if !opts.All {
				status.Rules = slices.DeleteFunc(status.Rules, func(u ratelimit.Usage) bool { return !u.Blocking })
			}

			statuses = append(statuses, layerStatus{Name: opts.LayerNames[i], Status: status})
		}

		writeWarnings(errOut, warnings)

		if opts.JSON {
			return json.NewEncoder(out).Encode(statuses)
		}

		return writeStatuses(out, statuses, time.Now())
	})
}

func RateLimitReset(cfg *config.Config, opts RateLimitResetOptions, out io.Writer, errOut io.Writer) error {
	return withLimiter(cfg, func(ctx context.Context, limiter *ratelimit.Limiter) error {
		used, warnings, err := limiter.ResetRule(ctx, opts.Rule, opts.Segments, opts.DryRun)
		writeWarnings(errOut, warnings)

		if err != nil {
			return err
		}

		if opts.JSON {
			return json.NewEncoder(out).Encode(map[string]any{"rule": opts.Rule, "segments": opts.Segments, "used": used, "dryRun": opts.DryRun})
		}

		var b strings.Builder

		if opts.DryRun {
			b.WriteString("Would reset ")
		} else {
			b.WriteString("Reset ")
		}

		b.WriteString(opts.Rule)

		var coords []string
		for _, s := range []string{ratelimit.SegmentUser, ratelimit.SegmentTenant, ratelimit.SegmentIP, ratelimit.SegmentLayer} {
			if v, ok := opts.Segments[s]; ok {
				coords = append(coords, s+"="+strconv.Quote(v))
			}
		}

		if len(coords) > 0 {
			b.WriteString(" for " + strings.Join(coords, " "))
		}

		fmt.Fprintf(out, "%v, which had %v used\n", b.String(), used)

		return nil
	})
}

// Only reads the configuration, so it works with any store
func RateLimitList(cfg *config.Config, opts RateLimitListOptions, out io.Writer) error {
	filter := opts.RuleFilter
	filter.Layers = make([]string, 0, len(opts.Layers))

	for _, name := range opts.Layers {
		id, ok := name, slices.ContainsFunc(cfg.Layers, func(l config.LayerConfig) bool { return l.ID == name })
		if !ok {
			id, ok = layerIDForName(cfg, name)
		}

		if !ok {
			return fmt.Errorf("invalid layer name: %v", name)
		}

		filter.Layers = append(filter.Layers, id)
	}

	ctx := pkg.BackgroundContext()
	messages := cfg.Error.Messages

	secreter, err := secrets.ConstructSecreter(configload.ReplaceEnv(cfg.Secret), secret.SecreterDeps{ErrorMessages: messages})
	if err != nil {
		return fmt.Errorf("error constructing secret: %w", err)
	}

	limiter, err := ratelimit.ConstructPolicies(ctx, *cfg, secreter, messages)
	if err != nil {
		return errors.Join(fmt.Errorf("error constructing ratelimit: %w", err), lifecycle.CloseIfCloser(ctx, secreter))
	}

	rules, err := limiter.Rules(filter)
	if err == nil {
		if opts.JSON {
			err = json.NewEncoder(out).Encode(rules)
		} else {
			err = writeRuleTable(out, rules)
		}
	}

	return errors.Join(err, lifecycle.CloseIfCloser(ctx, secreter))
}

// Connects to the rate limit store only, never to providers or caches
func withLimiter(cfg *config.Config, fn func(context.Context, *ratelimit.Limiter) error) error {
	ctx := pkg.BackgroundContext()
	messages := cfg.Error.Messages

	secreter, err := secrets.ConstructSecreter(configload.ReplaceEnv(cfg.Secret), secret.SecreterDeps{ErrorMessages: messages})
	if err != nil {
		return fmt.Errorf("error constructing secret: %w", err)
	}

	closeSecreter := func() error { return lifecycle.CloseIfCloser(ctx, secreter) }

	registry, err := datastores.ConstructDatastoreRegistry(ctx, storeDatastores(cfg), secreter, messages)
	if err != nil {
		return errors.Join(fmt.Errorf("error constructing datastores: %w", err), closeSecreter())
	}

	limiter, err := ratelimit.ConstructLimiter(ctx, *cfg, secreter, pkgratelimit.RateLimiterDeps{Datastores: registry, ErrorMessages: messages})
	if err != nil {
		return errors.Join(fmt.Errorf("error constructing ratelimit: %w", err), registry.Close(ctx), closeSecreter())
	}

	err = fn(ctx, limiter)

	return errors.Join(err, limiter.Close(ctx), registry.Close(ctx), closeSecreter())
}

// The first layer matching the name, the same way the server picks one
func layerIDForName(cfg *config.Config, name string) (string, bool) {
	for _, l := range cfg.Layers {
		if layers.ConfigMatchesName(l, cfg.Error.Messages, name) {
			return l.ID, true
		}
	}

	return "", false
}

// Only the datastore the store refers to, so the command doesn't connect to anything else
func storeDatastores(cfg *config.Config) []map[string]interface{} {
	id := ratelimit.StoreDatastoreID(cfg.RateLimit)
	if id == "" {
		return nil
	}

	for _, ds := range cfg.Datastores {
		if dsID, _ := configload.ReplaceEnv(ds)["id"].(string); dsID == id {
			return []map[string]interface{}{ds}
		}
	}

	return nil
}

func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		if !slices.Contains(list, v) {
			list = append(list, v)
		}
	}

	return list
}

func writeWarnings(out io.Writer, warnings []string) {
	for _, w := range warnings {
		fmt.Fprintln(out, "Warning: "+w)
	}
}

func formatSeconds(seconds int64) string {
	return (time.Duration(seconds) * time.Second).String()
}

func writeStatuses(out io.Writer, statuses []layerStatus, now time.Time) error {
	hasRules := false

	for _, s := range statuses {
		hasRules = hasRules || len(s.Rules) > 0

		switch {
		case s.Policy == "":
			fmt.Fprintf(out, "%v: allowed, not rate limited\n", s.Name)
		case s.Allowed:
			fmt.Fprintf(out, "%v: allowed\n", s.Name)
		default:
			var blocking []string
			for _, u := range s.Rules {
				if u.Blocking {
					blocking = append(blocking, u.Rule)
				}
			}

			retry := "won't be allowed until the limit changes"
			if s.RetryAfter != nil {
				retry = "retry after " + formatSeconds(*s.RetryAfter)
			}

			fmt.Fprintf(out, "%v: rejected by %v, %v\n", s.Name, strings.Join(blocking, ", "), retry)
		}
	}

	if !hasRules {
		return nil
	}

	fmt.Fprintln(out)

	return writeUsageTable(out, statuses, now)
}

func writeUsageTable(out io.Writer, statuses []layerStatus, now time.Time) error {
	tw := tabwriter.NewWriter(out, 0, 0, tablePadding, ' ', 0)

	fmt.Fprintln(tw, "Layer\tRule\tUsed\tLimit\tRemaining\tResets")

	for _, s := range statuses {
		for _, u := range s.Rules {
			limit := strconv.FormatInt(u.Limit, 10)
			remaining := strconv.FormatInt(u.Remaining, 10)

			if u.Unlimited {
				limit = "unlimited"
				remaining = "-"
			}

			resets := u.Resets.UTC().Format(time.RFC3339)
			if u.Rolling {
				resets = formatSeconds(int64(math.Ceil(u.Resets.Sub(now).Seconds())))
			}

			fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\t%v\n", s.Name, u.Rule, u.Used, limit, remaining, resets)
		}
	}

	return tw.Flush()
}

func writeRuleTable(out io.Writer, rules []ratelimit.RuleInfo) error {
	tw := tabwriter.NewWriter(out, 0, 0, tablePadding, ' ', 0)

	fmt.Fprintln(tw, "Rule\tLimit\tPeriod\tOverrides\tLayers")

	for _, r := range rules {
		period := r.Calendar
		if r.Period > 0 {
			period = formatSeconds(r.Period)
		}

		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\n", r.Rule, r.Limit, period, len(r.Overrides), strings.Join(r.Layers, ", "))
	}

	return tw.Flush()
}
