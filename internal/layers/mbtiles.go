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

package layers

import (
	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/twpayne/go-mbtiles"
)

type MBTilesConfig struct {
	Path string
}

type MBTiles struct {
	MBTilesConfig
	reader *mbtiles.Reader
}

func ConstructMBTiles(config MBTilesConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (*MBTiles, error) {
	reader, err := mbtiles.NewReader(config.Path)
	if err != nil {
		return nil, err
	}

	return &MBTiles{config, reader}, nil
}

func (t MBTiles) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return ProviderContext{AuthBypass: true}, nil
}

func (t MBTiles) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	img, err := t.reader.SelectTile(tileRequest.Z, tileRequest.X, tileRequest.Y)
	return &img, err
}
