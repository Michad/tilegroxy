package cmd

import (
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Commands related to the configuration",
}

func init() {
	rootCmd.AddCommand(configCmd)
}
