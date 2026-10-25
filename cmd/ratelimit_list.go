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

var rateLimitListCmd = &cobra.Command{
	Use:   "list [search]",
	Short: "List rate limit rules",
	Long: `Lists the configured rate limit rules matching every filter given, in
the order they're configured. With no filters every rule is listed.

The optional search matches any part of a rule's name, ignoring case.
Each flag matches a rule if it matches any of its values. --layer takes
a layer ID or a name from a tile request, and matches rules the layer
enforces including through extend. --segment matches rules segmented by
the value, or by nothing for "all".

This only reads the configuration, so it works with every store and
doesn't connect to one.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runRateLimitList,
}

func runRateLimitList(cmd *cobra.Command, args []string) {
	policies, err1 := cmd.Flags().GetStringSlice("policy")
	layerNames, err2 := cmd.Flags().GetStringSlice("layer")
	segments, err3 := cmd.Flags().GetStringSlice("segment")
	asJSON, err4 := cmd.Flags().GetBool("json")
	out := rootCmd.OutOrStdout()

	if err := errors.Join(err1, err2, err3, err4); err != nil {
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

	filter := ratelimit.RuleFilter{Policies: policies, Layers: layerNames, Segments: segments}
	if len(args) > 0 {
		filter.Search = args[0]
	}

	err = entry.RateLimitList(cfg, entry.RateLimitListOptions{RuleFilter: filter, JSON: asJSON}, out)

	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		exit(1)
	}
}

func init() {
	initRateLimitList()
}

func initRateLimitList() {
	rateLimitCmd.AddCommand(rateLimitListCmd)

	rateLimitListCmd.Flags().StringSliceP("policy", "p", nil, "Only rules enforced by these policies, including those they extend")
	rateLimitListCmd.Flags().StringSliceP("layer", "l", nil, "Only rules enforced on these layers")
	rateLimitListCmd.Flags().StringSlice("segment", nil, "Only rules segmented by these values. One of user, tenant, ip, layer, or all")
	rateLimitListCmd.Flags().Bool("json", false, "Output in JSON")
}
