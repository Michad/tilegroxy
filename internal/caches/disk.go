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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
)

type DiskConfig struct {
	Path     string
	FileMode uint32
}

type Disk struct {
	DiskConfig
}

func requestToFilename(t pkg.TileRequest) string {
	return t.StringWithSeparator("_")
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

func (s DiskRegistration) Initialize(configAny any, errorMessages config.ErrorMessages) (cache.Cache, error) {
	config := configAny.(DiskConfig)

	if config.Path == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "Cache.Disk.path", config.Path)
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

func (c Disk) Lookup(_ context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	filename := requestToFilename(t)

	b, err := os.ReadFile(filepath.Clean(filepath.Join(c.Path, filename)))

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return pkg.DecodeImage(b)
}

func (c Disk) Save(_ context.Context, t pkg.TileRequest, img *pkg.Image) error {
	filename := requestToFilename(t)
	b, err := img.Encode()

	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(c.Path, filename), b, fs.FileMode(c.FileMode))
}
