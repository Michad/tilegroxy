package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validates your configuration",
	Long:  `Checks the validity of the configuration you supplied and then exits, either with an exit code of 0 if valid or 1 if invalid`,
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
	},
}

func init() {
	configCmd.AddCommand(checkCmd)

	checkCmd.Flags().BoolP("verbose", "v", false, "Echos back the full configuration including default values")
}
