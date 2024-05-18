package caches

import (
	"errors"

	"github.com/Michad/tilegroxy/pkg"
)

type MultiConfig struct {
	Tiers []map[string]interface{}
}

type Multi struct {
	Tiers []Cache
}

func (c Multi) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	var allErrors error

	for _, cache := range c.Tiers {
		img, err := cache.Lookup(t)
		if err != nil {
			allErrors = errors.Join(allErrors, err)
		}

		if img != nil {
			return img, allErrors
		}
	}

	return nil, allErrors
}

func (c Multi) Save(t pkg.TileRequest, img *pkg.Image) error {
	var allErrors error

	for _, cache := range c.Tiers {
		err := cache.Save(t, img)
		allErrors = errors.Join(allErrors, err)
	}

	return allErrors
}
