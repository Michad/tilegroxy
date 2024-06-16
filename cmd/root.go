package cmd

import (
	"os"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tilegroxy",
	Short: "A service to proxy and cache map tile layers",
	Long: `Tilegroxy is an extensible CLI application that proxies mapping layers to external providers and adds cacheing and protection in front. 

	Tilegroxy is meant to be used to power "ZXY" tile layers commonly used in web mapping applications and only provides endpoints in this scheme.  
	However one use of tilegroxy is as an adapter to convert other mapping APIs such as WMS to a simple tile layer. Any API that returns georeferenced
	imagery can be used with tilegroxy.
	
	See the documentation at TODO for configuration instructions.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("config", "c", "./tilegroxy.yml", "A file path to the configuration file to use. The file should have an extension of either json or yml/yaml and be readable.")
}

func parseConfigIntoStructs(cmd *cobra.Command) (*config.Config, *layers.LayerGroup, *authentication.Authentication, error) {

	configPath, err := cmd.Flags().GetString("config")

	if err != nil {
		return nil, nil, nil, err
	}

	var cfg config.Config

	if configPath != "" {
		cfg, err = config.LoadConfigFromFile(configPath)

		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		panic("No configuration supplied")
	}

	layerGroup := layers.ConstructLayerGroup()

	callbackFunc := func(req internal.TileRequest) (*internal.Image, error) {
		return layerGroup.RenderTileNoCache(req)
	}

	cache, err := caches.ConstructCache(cfg.Cache, callbackFunc, &cfg.Error.Messages)
	if err != nil {
		return nil, nil, nil, err
	}

	auth, err := authentication.ConstructAuth(cfg.Authentication, &cfg.Error.Messages)
	if err != nil {
		return nil, nil, nil, err
	}

	for _, l := range cfg.Layers {
		layerObject, err := layers.ConstructLayer(l, &cfg.Error.Messages)
		if err != nil {
			return nil, nil, nil, err
		}

		layerObject.Cache = &cache
		if layerObject.Config.OverrideClient == nil {
			layerObject.Config.OverrideClient = &cfg.Client
		}

		layerGroup.AddLayer(layerObject)
	}

	if err != nil {
		return nil, nil, nil, err
	}

	return &cfg, layerGroup, &auth, err
}
