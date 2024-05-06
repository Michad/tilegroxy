package caches

import (
	"errors"
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
)

type Cache interface {
	Lookup(t pkg.TileRequest) (*pkg.Image, error)
	Save(t pkg.TileRequest, img *pkg.Image) error
}

func ConstructCache(rawConfig map[string]interface{}) (Cache, error) {
	if rawConfig["name"] == "None" {
		return NoopCache{}, nil
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, errors.New("Unsupported cache " + name)
}
