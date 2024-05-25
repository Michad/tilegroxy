package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/internal/server"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Starts a long-running process to service HTTP tile requests",
	Long: `Creates an HTTP server that listens for requests for tiles and serves them up in accordance with the configuration.

	The process will run blocking in the foreground until terminated. The majority of configuration should be supplied via a configuration file.
	
	`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("serve called")

		c, err := config.LoadConfigFromFile("./test_config.yml")

		if err != nil {
			log.Fatalf("error: %v", err)
		}

		fmt.Printf("--- t:\n%v\n\n", c)

		fmt.Printf("--- t:\n%v\n\n", c.Cache)

		cache, err := caches.ConstructCache(c.Cache, &c.Error.Messages)
		if err != nil {
			log.Fatalf("error: %v", err)
		}

		fmt.Printf("--- c:\n%v\n\n", cache)

		auth, err := authentication.ConstructAuth(c.Authentication, &c.Error.Messages)
		if err != nil {
			log.Fatalf("error: %v", err)
		}

		fmt.Printf("--- a:\n%v\n\n", auth)

		layerObjs := make([]*layers.Layer, len(c.Layers))

		for i, l := range c.Layers {
			layerObjs[i], err = layers.ConstructLayer(l, &c.Error.Messages)
			if err != nil {
				log.Fatalf("error: %v", err)
			}

			layerObjs[i].Cache = &cache
			if layerObjs[i].Config.OverrideClient == nil {
				layerObjs[i].Config.OverrideClient = &c.Client
			}
			fmt.Printf("--- l:\n%v\n\n", layerObjs[i])
		}

		if err != nil {
			log.Fatalf("error: %v", err)
		}

		err = server.ListenAndServe(c, layerObjs, &auth)

		if err != nil {
			log.Fatalf("error: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
