package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validates your configuration",
	Long:  `Checks the validity of the configuration you supplied and then exits. If everything is valid the program displays "Valid" and exits with a code of 0. If the configuration is invalid then a descriptive error is outputted and it exits with a non-zero status code.`,
	Run: func(cmd *cobra.Command, args []string) {
		verbose, _ := cmd.Flags().GetBool("verbose")

		cfg, _, _, err := parseConfigIntoStructs(cmd)

		if cfg != nil && verbose {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent(" ", "  ")
			enc.Encode(cfg)
		}

		if err != nil {
			panic(err)
		}

		println("Valid")
	},
}

func init() {
	configCmd.AddCommand(checkCmd)

	checkCmd.Flags().BoolP("echo", "e", false, "Echos back the full parsed configuration including default values if the configuration is valid")
}
