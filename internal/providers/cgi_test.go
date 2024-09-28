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

//go:build !unit

package providers

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CGI_Validate(t *testing.T) {
	cfg := CGIConfig{
		URI: "/?map=mapfiles/{layer.file}.map&MODE=tile&layers={layer.layer}&TILEMODE=gmap&TILE={x}+{y}+{z}",
	}

	cgi, err := CGIRegistration{}.Initialize(cfg, testClientConfig, testErrMessages, nil, nil)
	require.Error(t, err)
	assert.Nil(t, cgi)

	cfg = CGIConfig{
		Exec: "test_files/mapserv_via_docker.sh",
	}

	cgi, err = CGIRegistration{}.Initialize(cfg, testClientConfig, testErrMessages, nil, nil)
	require.Error(t, err)
	assert.Nil(t, cgi)
}

func Test_CGI_Mapserv(t *testing.T) {
	env := make(map[string]string)
	env["PATH"] = ""
	env["TEST"] = "HI"

	cfg := CGIConfig{
		Exec: "test_files/mapserv_via_docker.sh",
		URI:  "?map=mapfiles/{layer.file}.map&MODE=tile&layers={layer.layer}&TILEMODE=gmap&TILE={x}+{y}+{z}",
		Env:  env,
	}

	cgi, err := CGIRegistration{}.Initialize(cfg, testClientConfig, testErrMessages, nil, nil)

	require.NoError(t, err)
	assert.NotNil(t, cgi)

	ctx := pkg.BackgroundContext()
	ctxLayerPatternMatches, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*ctxLayerPatternMatches)["file"] = "states"
	(*ctxLayerPatternMatches)["layer"] = "all"

	pc, err := cgi.PreAuth(ctx, layer.ProviderContext{})
	require.NoError(t, err)

	img, err := cgi.GenerateTile(ctx, pc, pkg.TileRequest{LayerName: "states", Z: 8, X: 58, Y: 96})
	require.NoError(t, err)

	assert.NotNil(t, img)

}

func Test_CGI_InvalidMapserv(t *testing.T) {
	cfg := CGIConfig{
		Exec:           "test_files/mapserv_via_docker.sh",
		URI:            "/?map=mapfiles/{layer.file}.map&MODE=tile&layers={layer.layer}&TILEMODE=gmap&TILE={x}+{y}+{z}",
		InvalidAsError: true,
	}

	cgi, err := CGIRegistration{}.Initialize(cfg, testClientConfig, testErrMessages, nil, nil)

	require.NoError(t, err)
	assert.NotNil(t, cgi)

	ctx := pkg.BackgroundContext()
	ctxLayerPatternMatches, _ := pkg.LayerPatternMatchesFromContext(ctx)
	(*ctxLayerPatternMatches)["file"] = "fstates"
	(*ctxLayerPatternMatches)["layer"] = "all"

	pc, err := cgi.PreAuth(ctx, layer.ProviderContext{})
	require.NoError(t, err)

	img, err := cgi.GenerateTile(ctx, pc, pkg.TileRequest{LayerName: "states", Z: 8, X: 58, Y: 96})
	require.Error(t, err)

	assert.Nil(t, img)

}
