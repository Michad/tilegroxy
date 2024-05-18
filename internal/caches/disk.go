package caches

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
)

type DiskConfig struct {
	Path     string
	FileMode uint32
}

type Disk struct {
	config DiskConfig
}

func requestToFilename(t pkg.TileRequest) string {
	return t.LayerName + "_" + strconv.Itoa(t.Z) + "_" + strconv.Itoa(t.X) + "_" + strconv.Itoa(t.Y)
}

func ConstructDisk(config DiskConfig, ErrorMessages *config.ErrorMessages) (*Disk, error) {
	if config.Path == "" {
		return nil, fmt.Errorf(ErrorMessages.InvalidParam, "Cache.Disk.path", config.Path)
	}
	if config.FileMode == 0 {
		config.FileMode = 0777
	}

	err := os.MkdirAll(config.Path, fs.FileMode(config.FileMode))
	if err != nil {
		return nil, err
	}

	return &Disk{config}, nil
}

func (c Disk) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	filename := requestToFilename(t)

	img, err := os.ReadFile(filepath.Join(c.config.Path, filename))

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	return &img, err
}

func (c Disk) Save(t pkg.TileRequest, img *pkg.Image) error {
	filename := requestToFilename(t)

	return os.WriteFile(filepath.Join(c.config.Path, filename), *img, fs.FileMode(c.config.FileMode))
}
