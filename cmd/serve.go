package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Michad/tilegroxy/internal/server"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Starts a long-running process to service HTTP tile requests",
	Long: `Creates an HTTP server that listens for requests for tiles and serves them up in accordance with the configuration.

	The process will run blocking in the foreground until terminated. The majority of configuration should be supplied via a configuration file.
	
	`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, layerObjects, auth, err := parseConfigIntoStructs(cmd)

		if err != nil {
			fmt.Printf("Error: %v\n", err.Error())
			os.Exit(1)
		}

		err = server.ListenAndServe(cfg, layerObjects, auth)

		if err != nil {
			fmt.Printf("Error: %v\n", err.Error())
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
