package caches

import (
	"github.com/Michad/tilegroxy/pkg"
)

type Noop struct {
}

func (c Noop) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c Noop) Save(t pkg.TileRequest, img *pkg.Image) error {
	return nil
}
