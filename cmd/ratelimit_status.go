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

var rateLimitStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check whether a tile request would be rate limited",
	Long: `Checks whether the next tile request for each layer given with --layer
would be allowed for the given user, tenant, and IP address, without
counting a request. The layer is matched by name the same way the server
matches a request, including patterns.

Rejected requests list the rules blocking them and how long until the
request would be allowed. Pass --all to also show the rules that aren't
blocking, along with the limit after overrides and when the count resets.

A user or tenant left out is empty, the same as a request without one.
A rule needing an IP address you didn't supply is skipped with a warning.

For a rolling period, Used is the same weighted estimate the check uses.`,
	Run: runRateLimitStatus,
}

func runRateLimitStatus(cmd *cobra.Command, _ []string) {
	layerNames, err1 := cmd.Flags().GetStringSlice("layer")
	user, err2 := cmd.Flags().GetString("user")
	tenant, err3 := cmd.Flags().GetString("tenant")
	ip, err4 := cmd.Flags().GetString("ip")
	all, err5 := cmd.Flags().GetBool("all")
	asJSON, err6 := cmd.Flags().GetBool("json")
	out := rootCmd.OutOrStdout()

	if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
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

	err = entry.RateLimitStatus(cfg, entry.RateLimitStatusOptions{
		Request:    ratelimit.Request{User: user, Tenant: tenant, IP: ip},
		LayerNames: layerNames,
		All:        all,
		JSON:       asJSON,
	}, out, rootCmd.ErrOrStderr())

	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
	}
}

func init() {
	initRateLimitStatus()
}

func initRateLimitStatus() {
	rateLimitCmd.AddCommand(rateLimitStatusCmd)

	rateLimitStatusCmd.Flags().StringSliceP("layer", "l", nil, "The name(s) of the layer to check, as they'd appear in a tile request. Required")
	rateLimitStatusCmd.Flags().String("user", "", "The user ID to check as, standing in for what authentication would supply on a real request")
	rateLimitStatusCmd.Flags().String("tenant", "", "The tenant ID to check as, standing in for what authentication would supply on a real request")
	rateLimitStatusCmd.Flags().String("ip", "", "The client IP address to check as. Required by rules segmented by ip")
	rateLimitStatusCmd.Flags().BoolP("all", "a", false, "Show every rule that applies, not just the ones blocking the request")
	rateLimitStatusCmd.Flags().Bool("json", false, "Output in JSON")
}
