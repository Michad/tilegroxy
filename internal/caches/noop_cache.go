package caches

import (
	"github.com/Michad/tilegroxy/pkg"
)

type NoopCache struct {
}

func (c NoopCache) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c NoopCache) Save(t pkg.TileRequest, img *pkg.Image) error {
	return nil
}
