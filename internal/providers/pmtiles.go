// Copyright 2026 Michael Davis
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

package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"

	"github.com/Michad/tilegroxy/internal/pmtiles"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type PMTilesConfig struct {
	File string
	URL  string
}

type PMTiles struct {
	archive     *pmtiles.Archive
	contentType string
	metadata    config.LayerMetadata
}

func init() {
	layer.RegisterProvider(PMTilesRegistration{})
}

type PMTilesRegistration struct {
}

func (s PMTilesRegistration) InitializeConfig() any {
	return PMTilesConfig{}
}

func (s PMTilesRegistration) Name() string {
	return "pmtiles"
}

func (s PMTilesRegistration) DataType(_ any) config.DataType {
	return config.DataTypeUnknown
}

func (s PMTilesRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(PMTilesConfig)

	source, maxLength, err := openPMTilesSource(cfg, deps)
	if err != nil {
		return nil, err
	}

	// Initialize receives no context, and HTTP reads are bounded by the client timeout.
	archive, err := pmtiles.Open(context.Background(), source, maxLength)
	if err != nil {
		_ = source.Close()
		return nil, err
	}

	header := archive.Header()

	return &PMTiles{
		archive:     archive,
		contentType: header.TileType.ContentType(),
		metadata:    pmtilesMetadata(archive, pmtilesDataType(header.TileType)),
	}, nil
}

func pmtilesDataType(tileType pmtiles.TileType) config.DataType {
	switch tileType {
	case pmtiles.TileTypeMVT:
		return config.DataTypeMVT
	case pmtiles.TileTypePNG, pmtiles.TileTypeJPEG, pmtiles.TileTypeWebP, pmtiles.TileTypeAVIF:
		return config.DataTypeRaster
	case pmtiles.TileTypeUnknown, pmtiles.TileTypeMLT:
		return config.DataTypeUnknown
	default:
		return config.DataTypeUnknown
	}
}

// Local archives are operator-supplied, so the client MaxLength only guards remote reads.
func openPMTilesSource(cfg PMTilesConfig, deps layer.ProviderDeps) (pmtiles.Source, int, error) {
	switch {
	case cfg.File != "" && cfg.URL != "":
		return nil, 0, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "provider.pmtiles.file", "provider.pmtiles.url")
	case cfg.File != "":
		source, err := pmtiles.OpenFile(cfg.File)
		return source, math.MaxInt, err
	case cfg.URL != "":
		u, err := url.Parse(cfg.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, 0, fmt.Errorf(deps.ErrorMessages.InvalidParam, "provider.pmtiles.url", "")
		}
		return pmtiles.NewHTTPSource(cfg.URL, deps.ClientConfig), deps.ClientConfig.MaxLength, nil
	default:
		return nil, 0, fmt.Errorf(deps.ErrorMessages.OneOfRequired, []string{"provider.pmtiles.file", "provider.pmtiles.url"})
	}
}

func pmtilesMetadata(archive *pmtiles.Archive, dataType config.DataType) config.LayerMetadata {
	header := archive.Header()
	minZoom, maxZoom := int(header.MinZoom), int(header.MaxZoom)

	md := config.LayerMetadata{
		DataType: dataType,
		MinZoom:  &minZoom,
		MaxZoom:  &maxZoom,
	}

	// Archives omitting bounds or center leave them zeroed, which would otherwise read as null island.
	if bounds := header.Bounds(); bounds.West < bounds.East && bounds.South < bounds.North {
		md.Bounds = bounds.ToConfig()
	}

	if center := header.Center(); center[0] != 0 || center[1] != 0 || center[2] != 0 {
		md.Center = center
	}

	meta := archive.Metadata()
	md.Description, _ = meta["description"].(string)
	md.Attribution, _ = meta["attribution"].(string)
	md.Version, _ = meta["version"].(string)
	md.VectorLayers = pmtilesVectorLayers(meta["vector_layers"])

	return md
}

// Entries that don't match the TileJSON spec, such as non-string field descriptions, drop only themselves.
func pmtilesVectorLayers(raw any) []config.VectorLayer {
	entries, _ := raw.([]any)
	layers := make([]config.VectorLayer, 0, len(entries))

	for _, entry := range entries {
		b, err := json.Marshal(entry)
		if err != nil {
			continue
		}

		var l config.VectorLayer
		if json.Unmarshal(b, &l) == nil && l.ID != "" {
			layers = append(layers, l)
		}
	}

	if len(layers) == 0 {
		return nil
	}

	return layers
}

func (t *PMTiles) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t *PMTiles) GenerateTile(ctx context.Context, _ layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	missing := pmtiles.MissingTileError{Z: tileRequest.Z, X: tileRequest.X, Y: tileRequest.Y}

	z, x, y, ok := pmtilesCoordinates(tileRequest)
	if !ok {
		return nil, missing
	}

	data, found, err := t.archive.Tile(ctx, z, x, y)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, missing
	}

	return &pkg.Image{Content: data, ContentType: t.contentType}, nil
}

// The spec's Hilbert IDs only cover zoom 0 through 31.
func pmtilesCoordinates(r pkg.TileRequest) (uint8, uint32, uint32, bool) {
	if r.Z < 0 || r.Z > 31 || r.X < 0 || r.Y < 0 {
		return 0, 0, 0, false
	}

	limit := 1 << r.Z
	if r.X >= limit || r.Y >= limit {
		return 0, 0, 0, false
	}

	return uint8(r.Z), uint32(r.X), uint32(r.Y), true // #nosec G115 -- range checked above
}

func (t *PMTiles) Metadata() config.LayerMetadata {
	return t.metadata
}

func (t *PMTiles) Close(_ context.Context) error {
	return t.archive.Close()
}
