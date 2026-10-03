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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/pmtiles"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pmtilesMVTFixture = "../pmtiles/testdata/vector.pmtiles"
	pmtilesPNGFixture = "../pmtiles/testdata/raster.pmtiles"
)

func pmtilesDeps(client *config.ClientConfig) layer.ProviderDeps {
	cfg := config.DefaultConfig()
	if client != nil {
		cfg.Client = *client
	}
	return layer.ProviderDeps{ClientConfig: cfg.Client, ErrorMessages: cfg.Error.Messages}
}

func initPMTiles(t *testing.T, cfg PMTilesConfig, client *config.ClientConfig) *PMTiles {
	t.Helper()

	p, err := PMTilesRegistration{}.Initialize(cfg, pmtilesDeps(client))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.(*PMTiles).Close(context.Background()) })

	return p.(*PMTiles)
}

func Test_DataType_PMTiles(t *testing.T) {
	assert.Equal(t, config.DataTypeUnknown, PMTilesRegistration{}.DataType(PMTilesConfig{}))
}

func Test_PMTiles_DataTypeFromTileType(t *testing.T) {
	cases := map[pmtiles.TileType]config.DataType{
		pmtiles.TileTypeMVT:     config.DataTypeMVT,
		pmtiles.TileTypePNG:     config.DataTypeRaster,
		pmtiles.TileTypeJPEG:    config.DataTypeRaster,
		pmtiles.TileTypeWebP:    config.DataTypeRaster,
		pmtiles.TileTypeAVIF:    config.DataTypeRaster,
		pmtiles.TileTypeMLT:     config.DataTypeMLT,
		pmtiles.TileTypeUnknown: config.DataTypeUnknown,
		99:                      config.DataTypeUnknown,
	}

	for tt, want := range cases {
		assert.Equal(t, want, pmtilesDataType(tt), "type %d", tt)
	}
}

func Test_PMTiles_ConfigValidation(t *testing.T) {
	cases := map[string]PMTilesConfig{
		"neither":      {},
		"both":         {File: pmtilesPNGFixture, URL: "https://example.com/a.pmtiles"},
		"bad scheme":   {URL: "ftp://example.com/a.pmtiles"},
		"no host":      {URL: "https:///a.pmtiles"},
		"missing file": {File: "../pmtiles/testdata/missing.pmtiles"},
		"not archive":  {File: "test_files/single_pixel_red.png"},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := PMTilesRegistration{}.Initialize(cfg, pmtilesDeps(nil))
			require.Error(t, err)
		})
	}
}

func Test_PMTiles_FileMVT(t *testing.T) {
	p := initPMTiles(t, PMTilesConfig{File: pmtilesMVTFixture}, nil)

	img, err := p.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{Z: 0, X: 0, Y: 0})
	require.NoError(t, err)
	assert.Equal(t, mvtContentType, img.ContentType)
	assert.Len(t, img.Content, 51)

	md := p.Metadata()
	assert.Equal(t, config.DataTypeMVT, md.DataType)
	assert.Equal(t, 0, *md.MinZoom)
	assert.Equal(t, 1, *md.MaxZoom)
	assert.Equal(t, "Synthetic landmarks for tilegroxy tests", md.Description)
	assert.Equal(t, "tilegroxy", md.Attribution)
	assert.Equal(t, "1.2.0", md.Version)
	require.Len(t, md.VectorLayers, 1)
	assert.Equal(t, "landmarks", md.VectorLayers[0].ID)
	require.NotNil(t, md.Bounds)
	assert.InDelta(t, 15, md.Bounds.North, 0.0001)
	assert.InDelta(t, -10, md.Bounds.West, 0.0001)
	assert.Nil(t, md.Center)
}

func Test_PMTiles_FilePNG(t *testing.T) {
	p := initPMTiles(t, PMTilesConfig{File: pmtilesPNGFixture}, nil)

	img, err := p.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{Z: 2, X: 0, Y: 1})
	require.NoError(t, err)
	assert.Equal(t, "image/png", img.ContentType)

	md := p.Metadata()
	assert.Equal(t, config.DataTypeRaster, md.DataType)
	assert.Equal(t, []float64{12.5, -7.25, 2}, md.Center)
	assert.Empty(t, md.Description)
	assert.Nil(t, md.VectorLayers)
}

func Test_PMTiles_MissingTile(t *testing.T) {
	p := initPMTiles(t, PMTilesConfig{File: pmtilesPNGFixture}, nil)

	for _, req := range []pkg.TileRequest{{Z: 1, X: 1, Y: 1}, {Z: 2, X: 0, Y: 2}, {Z: 2, X: 5, Y: 0}, {Z: -1}, {Z: 40}} {
		_, err := p.GenerateTile(context.Background(), layer.ProviderContext{}, req)

		var missing pmtiles.MissingTileError
		require.ErrorAs(t, err, &missing, "%v", req)
	}
}

func Test_PMTiles_PreAuthBypasses(t *testing.T) {
	p := initPMTiles(t, PMTilesConfig{File: pmtilesPNGFixture}, nil)

	pc, err := p.PreAuth(context.Background(), layer.ProviderContext{})

	require.NoError(t, err)
	assert.True(t, pc.AuthBypass)
}

func Test_PMTiles_URL(t *testing.T) {
	data, err := os.ReadFile(pmtilesPNGFixture)
	require.NoError(t, err)

	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Api-Key")
		http.ServeContent(w, r, "a.pmtiles", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)

	client := config.DefaultConfig().Client
	client.Headers = map[string]string{"X-Api-Key": "secret"}
	p := initPMTiles(t, PMTilesConfig{URL: srv.URL + "/a.pmtiles"}, &client)

	img, err := p.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{Z: 2, X: 0, Y: 0})
	require.NoError(t, err)
	assert.Equal(t, "image/png", img.ContentType)
	assert.Equal(t, "secret", gotHeader)
}

func Test_PMTiles_FileIgnoresMaxLength(t *testing.T) {
	client := config.DefaultConfig().Client
	client.MaxLength = 1
	p := initPMTiles(t, PMTilesConfig{File: pmtilesPNGFixture}, &client)

	_, err := p.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{Z: 2, X: 0, Y: 1})
	require.NoError(t, err)
}

func Test_PMTiles_URLEnforcesMaxLength(t *testing.T) {
	data, err := os.ReadFile(pmtilesPNGFixture)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.pmtiles", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)

	client := config.DefaultConfig().Client
	client.MaxLength = 1
	p := initPMTiles(t, PMTilesConfig{URL: srv.URL + "/a.pmtiles"}, &client)

	_, err = p.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{Z: 2, X: 0, Y: 1})
	require.Error(t, err)
}

func Test_PMTiles_LayerUsesArchiveMetadata(t *testing.T) {
	cfg := config.DefaultConfig()
	rawConfig := config.LayerConfig{
		ID:       "pm",
		Provider: map[string]any{"name": "pmtiles", "file": pmtilesMVTFixture},
	}

	l, err := layer.ConstructLayer(context.Background(), rawConfig, cfg.Client, nil, cfg.Error.Messages, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Provider.(layer.ProviderWrapper).Close(context.Background()) })

	assert.Equal(t, config.DataTypeMVT, l.DataType)
	// The archive's zoom range is advertised but not enforced, since the layer sets no limits of its own.
	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 2}))
	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 1}))
	assert.Equal(t, 0, *l.Metadata().Advertised.MinZoom)
	assert.Equal(t, 1, *l.Metadata().Advertised.MaxZoom)
}

func Test_PMTiles_LayerBoundsKeepsArchiveMetadata(t *testing.T) {
	cfg := config.DefaultConfig()
	rawConfig := config.LayerConfig{
		ID:            "pm-bounds",
		LayerMetadata: config.LayerMetadata{DataType: config.DataTypeRaster, Bounds: config.BoundsConfig{South: -10, North: 10, West: -10, East: 10}},
		Provider:      map[string]any{"name": "pmtiles", "file": pmtilesPNGFixture},
	}

	l, err := layer.ConstructLayer(context.Background(), rawConfig, cfg.Client, nil, cfg.Error.Messages, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Provider.(layer.ProviderWrapper).Close(context.Background()) })

	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 1}))
	require.NoError(t, l.CheckZoomBounds(pkg.TileRequest{Z: 2}))
	assert.Equal(t, config.BoundsConfig{South: -10, North: 10, West: -10, East: 10}, l.Metadata().Limits.Bounds)
}

func Test_PMTilesVectorLayers_DropsNonConformingEntries(t *testing.T) {
	var raw any
	require.NoError(t, json.Unmarshal([]byte(`[
		{"id":"roads","fields":{"name":"String"},"minzoom":2,"maxzoom":14,"description":"streets"},
		{"id":"bad","fields":{"name":1}},
		{"fields":{}},
		"not an object"
	]`), &raw))

	layers := pmtilesVectorLayers(raw)

	require.Len(t, layers, 1)
	assert.Equal(t, "roads", layers[0].ID)
	assert.Equal(t, map[string]string{"name": "String"}, layers[0].Fields)
	assert.Equal(t, "streets", layers[0].Description)
	assert.Equal(t, 2, *layers[0].MinZoom)
	assert.Equal(t, 14, *layers[0].MaxZoom)

	assert.Nil(t, pmtilesVectorLayers(nil))
	assert.Nil(t, pmtilesVectorLayers(map[string]any{"id": "x"}))
}
