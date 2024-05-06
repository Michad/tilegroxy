package main

import (
	"fmt"
	"log"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"
)

func main() {
	c, err := config.LoadConfigFromFile("./test_config.yml")

	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("--- t:\n%v\n\n", c)

	fmt.Printf("--- t:\n%v\n\n", c.Cache)

	cache, err := caches.ConstructCache(c.Cache)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("--- c:\n%v\n\n", cache)

	auth, err := authentication.ConstructAuth(c.Authentication)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("--- a:\n%v\n\n", auth)

	layer, err := layers.ConstructLayer(c.Layers[0])

	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("--- l:\n%v\n\n", layer)

	layer.Cache = &cache

	internal.ListenAndServe(c, []*layers.Layer{layer}, &auth)
}
