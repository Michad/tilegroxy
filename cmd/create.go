package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates a bare-bones configuration",
	Long:  `Creates either a JSON or YAML configuration file with a skeleton you can use as a starting point for creating your configuration`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("create called")
	},
}

func init() {
	configCmd.AddCommand(createCmd)

	createCmd.Flags().Bool("force", false, "Overwrite the configuration file even if it exists and is non-empty. This will potentially overwrite your existing configuration!")
}
