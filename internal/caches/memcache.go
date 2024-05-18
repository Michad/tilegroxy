package caches

import (
	"github.com/Michad/tilegroxy/pkg"
)

type Memcache struct {
}

func (c Memcache) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c Memcache) Save(t pkg.TileRequest, img *pkg.Image) error {
	return nil
}
