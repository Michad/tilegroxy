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

package cmd

import (
	"errors"
	"fmt"

	"github.com/Michad/tilegroxy/internal/entry"
	"github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/spf13/cobra"
)

var rateLimitCmd = &cobra.Command{
	Use:   "ratelimit",
	Short: "Show or reset rate limit usage",
	Long: `Shows the current count for each rate limit rule that applies to the
given user, tenant, IP address, and layer, along with the limit after
overrides and when the count resets.

Rules come from the policies given with --policy, or the policy of the
layer given with --layer, including the policies they extend. Without
either, every policy is shown.

Supply the same values a tile request would have. A user or tenant left
out is empty, the same as a request without one. A rule needing an IP
address or layer you didn't supply is skipped with a warning.

For a rolling period, Used is the same weighted estimate the check uses.

Pass --reset to set the matching counters back to zero instead. For a
rolling period this clears both the current and previous bucket.`,
	Run: runRateLimit,
}

func runRateLimit(cmd *cobra.Command, _ []string) {
	ip, err1 := cmd.Flags().GetString("ip")
	asJSON, err2 := cmd.Flags().GetBool("json")
	layer, err3 := cmd.Flags().GetString("layer")
	policies, err4 := cmd.Flags().GetStringSlice("policy")
	reset, err5 := cmd.Flags().GetBool("reset")
	dryRun, err6 := cmd.Flags().GetBool("dry-run")
	tenant, err7 := cmd.Flags().GetString("tenant")
	user, err8 := cmd.Flags().GetString("user")
	out := rootCmd.OutOrStdout()

	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8); err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
		return
	}

	cfg, err := extractConfigFromCommand(cmd, nil)
	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
		return
	}

	err = entry.RateLimit(cfg, entry.RateLimitOptions{
		InspectOptions: ratelimit.InspectOptions{
			Policies: policies,
			Layer:    layer,
			User:     user,
			Tenant:   tenant,
			IP:       ip,
		},
		JSON:   asJSON,
		Reset:  reset,
		DryRun: dryRun,
	}, out, rootCmd.ErrOrStderr())

	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
	}
}

func init() {
	initRateLimit()
}

func initRateLimit() {
	rootCmd.AddCommand(rateLimitCmd)

	rateLimitCmd.Flags().String("ip", "", "The client IP address to look up")
	rateLimitCmd.Flags().Bool("json", false, "Output in JSON")
	rateLimitCmd.Flags().StringP("layer", "l", "", "The ID of the layer to look up. Selects the layer's policy unless --policy is given. Required by rules segmented by layer")
	rateLimitCmd.Flags().StringSliceP("policy", "p", nil, "The ID(s) of the policies to look up. Every policy by default")
	rateLimitCmd.Flags().Bool("reset", false, "Set the matching counters back to zero")
	rateLimitCmd.Flags().Bool("dry-run", false, "Only usable with --reset. Display what rules will be reset")
	rateLimitCmd.Flags().String("tenant", "", "The tenant ID to look up")
	rateLimitCmd.Flags().String("user", "", "The user ID to look up")
}
