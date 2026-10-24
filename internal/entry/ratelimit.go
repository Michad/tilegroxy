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
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/Michad/tilegroxy/internal/secrets"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	pkgratelimit "github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

type RateLimitOptions struct {
	ratelimit.InspectOptions
	JSON   bool
	Reset  bool
	DryRun bool
}

const tablePadding = 4

// Connects to the rate limit store only, never to providers or caches
func RateLimit(cfg *config.Config, opts RateLimitOptions, out io.Writer, errOut io.Writer) error {
	if opts.DryRun && !opts.Reset {
		return errors.New("--dry-run can only be used with --reset")
	}

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

	if opts.Reset {
		err = resetRateLimit(ctx, limiter, opts, out, errOut)
	} else {
		err = inspectRateLimit(ctx, limiter, opts, out, errOut)
	}

	return errors.Join(err, limiter.Close(ctx), registry.Close(ctx), closeSecreter())
}

func inspectRateLimit(ctx context.Context, limiter *ratelimit.Limiter, opts RateLimitOptions, out io.Writer, errOut io.Writer) error {
	usages, warnings, err := limiter.Inspect(ctx, opts.InspectOptions)
	writeWarnings(errOut, warnings)

	if err != nil {
		return err
	}

	if opts.JSON {
		return json.NewEncoder(out).Encode(usages)
	}

	return writeUsageTable(out, usages, time.Now())
}

func resetRateLimit(ctx context.Context, limiter *ratelimit.Limiter, opts RateLimitOptions, out io.Writer, errOut io.Writer) error {
	rules, warnings, err := limiter.Reset(ctx, opts.InspectOptions, opts.DryRun)
	writeWarnings(errOut, warnings)

	if err != nil {
		return err
	}

	if opts.JSON {
		return json.NewEncoder(out).Encode(map[string]any{"reset": rules, "dryRun": opts.DryRun})
	}

	verb := "Reset"
	if opts.DryRun {
		verb = "Would reset"
	}

	for _, r := range rules {
		fmt.Fprintln(out, verb+" "+r)
	}

	return nil
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

func writeWarnings(out io.Writer, warnings []string) {
	for _, w := range warnings {
		fmt.Fprintln(out, "Warning: "+w)
	}
}

func writeUsageTable(out io.Writer, usages []ratelimit.Usage, now time.Time) error {
	tw := tabwriter.NewWriter(out, 0, 0, tablePadding, ' ', 0)

	fmt.Fprintln(tw, "Rule\tUsed\tLimit\tRemaining\tResets")

	for _, u := range usages {
		limit := strconv.FormatInt(u.Limit, 10)
		remaining := strconv.FormatInt(u.Remaining, 10)

		if u.Unlimited {
			limit = "unlimited"
			remaining = "-"
		}

		resets := u.Resets.UTC().Format(time.RFC3339)
		if u.Rolling {
			resets = (time.Duration(math.Ceil(u.Resets.Sub(now).Seconds())) * time.Second).String()
		}

		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\n", u.Rule, u.Used, limit, remaining, resets)
	}

	return tw.Flush()
}
