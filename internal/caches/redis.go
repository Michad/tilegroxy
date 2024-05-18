package caches

import (
	"github.com/Michad/tilegroxy/pkg"
)

type Redis struct {
}

func (c Redis) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c Redis) Save(t pkg.TileRequest, img *pkg.Image) error {
	return nil
}
