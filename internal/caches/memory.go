package caches

import (
	"time"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"

	"github.com/maypok86/otter"
)

type MemoryConfig struct {
	MaxSize uint16 //Maximum number of tiles to hold in the cache. Defaults to 100
	Ttl     uint32 //Maximum time to live of a tile in seconds. Defaultss to 3600 (1 hour)
}

type Memory struct {
	Config MemoryConfig
	Cache  otter.Cache[string, []byte]
}

func ConstructMemory(config MemoryConfig, ErrorMessages *config.ErrorMessages) (*Memory, error) {
	if config.MaxSize < 1 {
		config.MaxSize = 100
	}

	if config.Ttl < 1 {
		config.Ttl = 3600
	}

	cache, err := otter.MustBuilder[string, pkg.Image](int(config.MaxSize)).
		Cost(func(key string, value pkg.Image) uint32 {
			return uint32(len(value))
		}).
		WithTTL(time.Duration(config.Ttl * uint32(time.Second))).
		Build()
	if err != nil {
		return nil, err
	}

	return &Memory{config, cache}, nil
}

func (c Memory) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c Memory) Save(t pkg.TileRequest, img *pkg.Image) error {
	return nil
}
