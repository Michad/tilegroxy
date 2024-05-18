package caches

import (
	"errors"
	"fmt"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/mitchellh/mapstructure"
)

type Cache interface {
	Lookup(t pkg.TileRequest) (*pkg.Image, error)
	Save(t pkg.TileRequest, img *pkg.Image) error
}

func ConstructCache(rawConfig map[string]interface{}, ErrorMessages *config.ErrorMessages) (Cache, error) {
	if rawConfig["name"] == "None" || rawConfig["name"] == "Test" {
		return Noop{}, nil
	} else if rawConfig["name"] == "Disk" {
		var config DiskConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructDisk(config, ErrorMessages)
	} else if rawConfig["name"] == "Memory" {
		var config MemoryConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructMemory(config, ErrorMessages)
	} else if rawConfig["name"] == "Multi" {
		var config MultiConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		tierCaches := make([]Cache, len(config.Tiers))

		for i, tierRawConfig := range config.Tiers {
			tierCache, err := ConstructCache(tierRawConfig, ErrorMessages)

			if err != nil {
				return nil, err
			}

			tierCaches[i] = tierCache
		}

		return Multi{tierCaches}, nil
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, errors.New("Unsupported cache " + name)
}
