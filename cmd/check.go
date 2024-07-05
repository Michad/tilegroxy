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
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validates your configuration",
	Long:  `Checks the validity of the configuration you supplied and then exits. If everything is valid the program displays "Valid" and exits with a code of 0. If the configuration is invalid then a descriptive error is outputted and it exits with a non-zero status code.`,
	Run:   runCheck,
}

func runCheck(cmd *cobra.Command, args []string) {
	echo, _ := cmd.Flags().GetBool("echo")
	out := cmd.OutOrStdout()

	cfg, _, _, err := parseConfigIntoStructs(cmd)

	if err != nil {
		fmt.Fprintf(out, "Invalid configuration: %v\n", err.Error())
		exit(1)
		return
	}

	if cfg != nil && echo {
		enc := json.NewEncoder(out)
		enc.SetIndent(" ", "  ")
		enc.Encode(cfg)
	} else {
		fmt.Fprintln(out, "Valid")
	}
}

func init() {
	initCheck()
}

func initCheck() {
	configCmd.AddCommand(checkCmd)

	checkCmd.Flags().BoolP("echo", "e", false, "Echos back the full parsed configuration including default values if the configuration is valid")
}
