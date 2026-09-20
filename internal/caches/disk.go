// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package caches

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
)

type DiskLayout string

const (
	DiskLayoutFlat       = "flat"       // Every tile is a file directly inside path
	DiskLayoutCoordinate = "coordinate" // Tiles are nested in a layer/z/x/y directory tree
)

var allDiskLayouts = []DiskLayout{DiskLayoutFlat, DiskLayoutCoordinate}

type DiskConfig struct {
	Path     string
	FileMode uint32
	Layout   DiskLayout
}

type Disk struct {
	DiskConfig
}

// For the "flat" layout - layer name and tile coordinates in one filename
func requestToFilename(t pkg.TileRequest) string {
	safe := t
	safe.LayerName = safeLayerName(t.LayerName)
	return safe.StringWithSeparator("_")
}

// Returns the directory holding the tile and the tile's filename within it, both relative to Path. For the "coordinate" layout
func (c Disk) requestToPath(t pkg.TileRequest) (string, string) {
	if c.Layout == DiskLayoutCoordinate {
		dir := filepath.Join(safeLayerName(t.LayerName), strconv.Itoa(t.Z), strconv.Itoa(t.X))
		return filepath.Join(c.Path, dir), strconv.Itoa(t.Y)
	}

	return c.Path, requestToFilename(t)
}

func init() {
	cache.RegisterCache(DiskRegistration{})
}

type DiskRegistration struct {
}

func (s DiskRegistration) InitializeConfig() any {
	return DiskConfig{}
}

func (s DiskRegistration) Name() string {
	return "disk"
}

func (s DiskRegistration) Initialize(configAny any, deps cache.CacheDeps) (cache.Cache, error) {
	config := configAny.(DiskConfig)

	if config.Path == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "Cache.Disk.path", config.Path)
	}
	if config.FileMode == 0 {
		config.FileMode = 0777
	}
	if config.Layout == "" {
		config.Layout = DiskLayoutFlat
	}
	if !slices.Contains(allDiskLayouts, config.Layout) {
		return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "cache.disk.layout", config.Layout, allDiskLayouts)
	}

	err := os.MkdirAll(config.Path, fs.FileMode(config.FileMode))
	if err != nil {
		return nil, err
	}

	return &Disk{config}, nil
}

func (c Disk) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	dir, filename := c.requestToPath(t)

	b, err := os.ReadFile(filepath.Clean(filepath.Join(dir, filename)))

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return pkg.DecodeImage(b)
}

func (c Disk) Save(_ context.Context, t pkg.TileRequest, img *pkg.Image) error {
	dir, filename := c.requestToPath(t)
	b, err := img.Encode()

	if err != nil {
		return err
	}

	if dir != c.Path {
		if err = os.MkdirAll(dir, fs.FileMode(c.FileMode)); err != nil {
			return err
		}
	}

	// Write to a temp file and rename so an interrupted write never leaves a readable partial entry
	dest := filepath.Clean(filepath.Join(dir, filename))

	tmp, err := os.CreateTemp(dir, filename+".tmp")

	if err != nil {
		return err
	}

	tmpName := tmp.Name()

	// Both fail in the success path: the rename already consumed the temp file and closed it.
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if err = tmp.Chmod(fs.FileMode(c.FileMode)); err != nil {
		return err
	}

	if _, err = tmp.Write(b); err != nil {
		return err
	}

	if err = tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, dest)
}

func (c Disk) Remove(_ context.Context, t pkg.TileRequest) (bool, error) {
	dir, filename := c.requestToPath(t)

	err := os.Remove(filepath.Clean(filepath.Join(dir, filename)))

	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}
