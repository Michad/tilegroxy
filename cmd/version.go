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
	Run: func(cmd *cobra.Command, args []string) {
		short, _ := cmd.Flags().GetBool("short")
		json, _ := cmd.Flags().GetBool("json")

		version, ref, date := internal.GetVersionInformation()

		if json {
			if short {
				fmt.Println("{\"version\": \"" + version + "\"}")
			} else {
				fmt.Printf("{\n  \"version\": \"%v\",\n  \"ref\": \"%v\",\n  \"goVersion\": \"%v\",\n  \"buildDate\": \"%v\"\n}\n", version, ref, runtime.Version(), date)
			}
		} else {
			if short {
				fmt.Println(version)
			} else {
				fmt.Printf("tilegroxy/%v-%v %v\nBuilt at %v\n", version, ref, runtime.Version(), date)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)

	versionCmd.Flags().Bool("short", false, "Include just the tilegroxy version by itself")
	versionCmd.Flags().Bool("json", false, "Output version information in JSON format")
}
