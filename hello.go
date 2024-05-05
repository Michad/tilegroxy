package main

import (
	"fmt"
	"log"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/providers"
	"github.com/mitchellh/mapstructure"
)

func main() {
	c, err := config.LoadConfigFromFile("./test_config.yml")

	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("--- t:\n%v\n\n", c)

	var inter providers.Provider
	var result providers.UrlTemplate
	err = mapstructure.Decode(c.Layers[0].Provider, &result)

	if err != nil {
		log.Fatalf("error: %v", err)
	}

	inter = result

	fmt.Printf("--- t:\n%v\n\n", inter)

	internal.ListenAndServe(c)
}
