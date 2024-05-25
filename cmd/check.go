package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validates your configuration",
	Long:  `Checks the validity of the configuration you supplied and then exits, either with an exit code of 0 if valid or 1 if invalid`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("check called")
	},
}

func init() {
	configCmd.AddCommand(checkCmd)
}
