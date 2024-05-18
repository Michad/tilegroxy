package caches

import "github.com/Michad/tilegroxy/pkg"

type S3 struct {
}

func (c S3) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c S3) Save(t pkg.TileRequest, img *pkg.Image) error {
	return nil
}
