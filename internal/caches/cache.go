package caches

import (
	"fmt"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/mitchellh/mapstructure"
)

type Cache interface {
	Lookup(t pkg.TileRequest) (*pkg.Image, error)
	Save(t pkg.TileRequest, img *pkg.Image) error
}

func ConstructCache(rawConfig map[string]interface{}, errorMessages *config.ErrorMessages) (Cache, error) {
	if rawConfig["name"] == "none" || rawConfig["name"] == "test" {
		return Noop{}, nil
	} else if rawConfig["name"] == "disk" {
		var config DiskConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructDisk(config, errorMessages)
	} else if rawConfig["name"] == "memory" {
		var config MemoryConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructMemory(config, errorMessages)
	} else if rawConfig["name"] == "group" {
		var config GroupConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructGroupCache(config, errorMessages)
	} else if rawConfig["name"] == "multi" {
		var config MultiConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		tierCaches := make([]Cache, len(config.Tiers))

		for i, tierRawConfig := range config.Tiers {
			tierCache, err := ConstructCache(tierRawConfig, errorMessages)

			if err != nil {
				return nil, err
			}

			tierCaches[i] = tierCache
		}

		return Multi{tierCaches}, nil
	} else if rawConfig["name"] == "s3" {
		var config S3Config
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructS3(&config, errorMessages)
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.InvalidParam, "cache.name", name)
}
