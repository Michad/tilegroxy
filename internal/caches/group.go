package caches

import (
	"fmt"

	"github.com/Michad/tilegroxy/internal/caches/group"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
)

// GroupConfig is a local type so callers needn't import caches/group directly.
type GroupConfig group.Config

// GroupCache is a caches.Cache to use a groupcache as a cache.
type GroupCache struct {
	*group.Cache
}

// Lookup takes a TileRequest and returns an Image or an error.
func (g *GroupCache) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	file := fmt.Sprintf("%d/%d/%d", t.Z, t.X, t.Y) // TODO
	if v, ok := g.Cache.Get(t.LayerName, file); ok {
		i := v.(pkg.Image)
		return &i, nil
	}
	return nil, group.ItemNotFoundError
}

// Save takes a TileRequest and an Image, and returns an error if it cannot be set.
func (g *GroupCache) Save(t pkg.TileRequest, img *pkg.Image) error {
	file := fmt.Sprintf("%d/%d/%d", t.Z, t.X, t.Y) // TODO
	return g.Cache.Set(t.LayerName, file, *img)
}

// NewGroupCache creates a new GroupCache from the specified config and returns it, or returns an error.
func ConstructGroupCache(conf GroupConfig, errorMessages *config.ErrorMessages) (*GroupCache, error) {

	// GroupCache has the concept of a backfill, whereby it will pull from another source on cache miss,
	// helping reduce hotspots and distributing the pull load.
	//
	// This feature isn't compatible with the []Cache concept, as is, so we set the backfill to nil, and
	// instead will manually "Save()"
	gc, err := group.NewCache(group.Config(conf), nil)
	if err != nil {
		return nil, err
	}
	return &GroupCache{
		Cache: gc,
	}, nil
}
