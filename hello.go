package main

import (
	"fmt"
	"log"

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/internal/server"
)

func main() {
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

}
