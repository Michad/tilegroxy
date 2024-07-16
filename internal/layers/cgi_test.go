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
	"testing"

	"github.com/Michad/tilegroxy/internal"
	"github.com/stretchr/testify/assert"
)

func Test_CGI_Validate(t *testing.T) {
	cfg := CGIConfig{
		Uri: "/?map=mapfiles/{layer.file}.map&MODE=tile&layers={layer.layer}&TILEMODE=gmap&TILE={x}+{y}+{z}",
	}

	cgi, err := ConstructCGI(cfg, testClientConfig, testErrMessages)
	assert.Error(t, err)
	assert.Nil(t, cgi)

	cfg = CGIConfig{
		Exec: "test_files/mapserv_via_docker.sh",
	}

	cgi, err = ConstructCGI(cfg, testClientConfig, testErrMessages)
	assert.Error(t, err)
	assert.Nil(t, cgi)
}

func Test_CGI_Mapserv(t *testing.T) {
	env := make(map[string]string)
	env["PATH"] = ""
	env["TEST"] = "HI"

	cfg := CGIConfig{
		Exec: "test_files/mapserv_via_docker.sh",
		Uri:  "/?map=mapfiles/{layer.file}.map&MODE=tile&layers={layer.layer}&TILEMODE=gmap&TILE={x}+{y}+{z}",
		Env:  env,
	}

	cgi, err := ConstructCGI(cfg, testClientConfig, testErrMessages)

	assert.NoError(t, err)
	assert.NotNil(t, cgi)

	ctx := internal.BackgroundContext()
	ctx.LayerPatternMatches["file"] = "states"
	ctx.LayerPatternMatches["layer"] = "all"

	pc, err := cgi.PreAuth(ctx, ProviderContext{})
	assert.NoError(t, err)

	img, err := cgi.GenerateTile(ctx, pc, internal.TileRequest{LayerName: "states", Z: 8, X: 58, Y: 96})
	assert.NoError(t, err)

	assert.NotNil(t, img)

}

func Test_CGI_InvalidMapserv(t *testing.T) {
	cfg := CGIConfig{
		Exec:           "test_files/mapserv_via_docker.sh",
		Uri:            "/?map=mapfiles/{layer.file}.map&MODE=tile&layers={layer.layer}&TILEMODE=gmap&TILE={x}+{y}+{z}",
		InvalidAsError: true,
	}

	cgi, err := ConstructCGI(cfg, testClientConfig, testErrMessages)

	assert.NoError(t, err)
	assert.NotNil(t, cgi)

	ctx := internal.BackgroundContext()
	ctx.LayerPatternMatches["file"] = "fstates"
	ctx.LayerPatternMatches["layer"] = "all"

	pc, err := cgi.PreAuth(ctx, ProviderContext{})
	assert.NoError(t, err)

	img, err := cgi.GenerateTile(ctx, pc, internal.TileRequest{LayerName: "states", Z: 8, X: 58, Y: 96})
	assert.Error(t, err)

	assert.Nil(t, img)

}
