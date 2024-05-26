package cmd

import (
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
			panic(err)
		}

		err = server.ListenAndServe(cfg, layerObjects, auth)

		if err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
