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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type DiskConfig struct {
	Path     string
	FileMode uint32
}

type Disk struct {
	DiskConfig
}

func requestToFilename(t internal.TileRequest) string {
	return t.LayerName + "_" + strconv.Itoa(t.Z) + "_" + strconv.Itoa(t.X) + "_" + strconv.Itoa(t.Y)
}

func ConstructDisk(config DiskConfig, ErrorMessages config.ErrorMessages) (*Disk, error) {
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

func (c Disk) Lookup(t internal.TileRequest) (*internal.Image, error) {
	filename := requestToFilename(t)

	img, err := os.ReadFile(filepath.Join(c.Path, filename))

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	return &img, err
}

func (c Disk) Save(t internal.TileRequest, img *internal.Image) error {
	filename := requestToFilename(t)

	return os.WriteFile(filepath.Join(c.Path, filename), *img, fs.FileMode(c.FileMode))
}
