package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Outputs version information",
	Long:  `Outputs information about the version of tilegroxy being used.`,
	Run: func(cmd *cobra.Command, args []string) {
		short, _ := cmd.Flags().GetBool("short")

		version := "0.0.1" //TODO: Make tilegroxy version dynamic

		if short {
			fmt.Println(version)
		} else {
			fmt.Printf("tilegroxy/%v %v\n", version, runtime.Version())
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)

	versionCmd.Flags().Bool("short", false, "Include just the tilegroxy version by itself")
}
