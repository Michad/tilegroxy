package main

import (
	"fmt"
	"log"

	"gopkg.in/yaml.v3"
)

var sample = `
cache:
    name: Test
    verbose: true
layers:
    -
        id: test
        provider:
            name: url template
            template: http://example.com/?bbox=$xmin,$ymin,$xmax,$ymax
    -
        id: test2
        provider:
            name: url template
            template: http://example.com/?bbox=$xmin,$ymin,$xmax,$ymax
`

type Config struct {
	Cache struct {
		Name    string
		Verbose bool
	}
	Layers []struct {
		Id       string
		Provider struct {
			Name     string
			Template string
		}
	}
}

func main() {
	t := Config{}

	err := yaml.Unmarshal([]byte(sample), &t)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	fmt.Printf("--- t:\n%v\n\n", t)
}
