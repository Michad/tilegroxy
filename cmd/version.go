// Copyright 2024 Michael Davis
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
	"fmt"
	"runtime"

	"github.com/Michad/tilegroxy/internal"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Outputs version information",
	Long:  `Outputs information about the version of tilegroxy being used.`,
	Run:   runVersion,
}

func runVersion(cmd *cobra.Command, args []string) {
	short, _ := cmd.Flags().GetBool("short")
	json, _ := cmd.Flags().GetBool("json")

	version, ref, date := internal.GetVersionInformation()
	out := rootCmd.OutOrStdout()

	if json {
		if short {
			fmt.Fprintln(out, "{\"version\": \""+version+"\"}")
		} else {
			fmt.Fprintf(out, "{\n  \"version\": \"%v\",\n  \"ref\": \"%v\",\n  \"goVersion\": \"%v\",\n  \"buildDate\": \"%v\"\n}\n", version, ref, runtime.Version(), date)
		}
	} else {
		if short {
			fmt.Fprintln(out, version)
		} else {
			fmt.Fprintf(out, "tilegroxy/%v-%v %v\nBuilt at %v\n", version, ref, runtime.Version(), date)
		}
	}
}

func init() {
	initVersion()
}

func initVersion() {
	rootCmd.AddCommand(versionCmd)

	versionCmd.Flags().Bool("short", false, "Include just the tilegroxy version by itself")
	versionCmd.Flags().Bool("json", false, "Output version information in JSON format")
}
