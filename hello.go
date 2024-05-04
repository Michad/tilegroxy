package main

import (
	"fmt"
	"log"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/providers"
	"github.com/mitchellh/mapstructure"
)

func main() {
	t := config.Config{}
	err := config.LoadConfigFromFile("./examples/configurations/simple.yml", &t)

	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("--- t:\n%v\n\n", t)

	var inter providers.Provider
	var result providers.UrlTemplate
	err = mapstructure.Decode(t.Layers[0].Provider, &result)

	if err != nil {
		log.Fatalf("error: %v", err)
	}

	inter = result

	fmt.Printf("--- t:\n%v\n\n", inter)
}
