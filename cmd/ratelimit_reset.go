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

var rateLimitResetCmd = &cobra.Command{
	Use:   "reset <rule>",
	Short: "Set one rate limit counter back to zero",
	Long: `Sets the counter of one rate limit rule back to zero. For a rolling
period this clears both the current and previous bucket.

Give the rule by name, such as premium.tenant.month, which
'ratelimit list' shows. Every segment of the rule must be supplied with
its flag, and only those. Pass an empty value, such as --user "", to
reset the counter shared by requests without one. --layer takes the
layer's ID, not the name from the URL.`,
	Args: cobra.ExactArgs(1),
	Run:  runRateLimitReset,
}

func runRateLimitReset(cmd *cobra.Command, args []string) {
	dryRun, err1 := cmd.Flags().GetBool("dry-run")
	asJSON, err2 := cmd.Flags().GetBool("json")
	out := rootCmd.OutOrStdout()

	segments := map[string]string{}
	errs := []error{err1, err2}

	for _, s := range []string{ratelimit.SegmentUser, ratelimit.SegmentTenant, ratelimit.SegmentIP, ratelimit.SegmentLayer} {
		if cmd.Flags().Changed(s) {
			v, err := cmd.Flags().GetString(s)
			segments[s] = v
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
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

	err = entry.RateLimitReset(cfg, entry.RateLimitResetOptions{
		Rule:     args[0],
		Segments: segments,
		DryRun:   dryRun,
		JSON:     asJSON,
	}, out, rootCmd.ErrOrStderr())

	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
	}
}

func init() {
	initRateLimitReset()
}

func initRateLimitReset() {
	rateLimitCmd.AddCommand(rateLimitResetCmd)

	rateLimitResetCmd.Flags().String("user", "", "The user ID of the counter. Required when the rule is segmented by user")
	rateLimitResetCmd.Flags().String("tenant", "", "The tenant ID of the counter. Required when the rule is segmented by tenant")
	rateLimitResetCmd.Flags().String("ip", "", "The client IP address of the counter. Required when the rule is segmented by ip")
	rateLimitResetCmd.Flags().StringP("layer", "l", "", "The layer ID of the counter. Required when the rule is segmented by layer")
	rateLimitResetCmd.Flags().Bool("dry-run", false, "Show the counter that would be reset without changing it")
	rateLimitResetCmd.Flags().Bool("json", false, "Output in JSON")
}
