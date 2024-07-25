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
	"net/http"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TODO: change this to something less likely to change
const testTemplate = "https://tigerweb.geo.census.gov/arcgis/services/TIGERweb/tigerWMS_PhysicalFeatures/MapServer/WMSServer?VERSION=1.3.0&SERVICE=WMS&REQUEST=GetMap&LAYERS=19&STYLES=&CRS=$srs&BBOX=$xmin,$ymin,$xmax,$ymax&WIDTH=$width&HEIGHT=$height&FORMAT=image/png"

func Test_UrlTemplateValidate(t *testing.T) {
	p, err := URLTemplateRegistration{}.Initialize(URLTemplateConfig{}, config.ClientConfig{}, testErrMessages, nil)

	assert.Nil(t, p)
	require.Error(t, err)
}
func Test_UrlTemplateExecute(t *testing.T) {
	p, err := URLTemplateRegistration{}.Initialize(URLTemplateConfig{Template: testTemplate}, config.ClientConfig{StatusCodes: []int{http.StatusOK}, MaxLength: 2000, ContentTypes: []string{"image/png"}, UnknownLength: true}, testErrMessages, nil)

	assert.NotNil(t, p)
	require.NoError(t, err)

	pc, err := p.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := p.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.NotNil(t, img)
	require.NoError(t, err)
}

func Test_UrlTemplateConfigOptions(t *testing.T) {
	var clientConfig = config.ClientConfig{StatusCodes: []int{400}, MaxLength: 2000, ContentTypes: []string{"image/png"}, UnknownLength: true}
	p, err := URLTemplateRegistration{}.Initialize(URLTemplateConfig{Template: testTemplate}, clientConfig, testErrMessages, nil)
	assert.NotNil(t, p)
	require.NoError(t, err)

	img, err := p.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	require.Error(t, err)

	clientConfig.StatusCodes = []int{http.StatusOK}
	clientConfig.MaxLength = 2
	img, err = p.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	require.Error(t, err)

	clientConfig.MaxLength = 2000
	clientConfig.UnknownLength = false
	img, err = p.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	require.Error(t, err)

	clientConfig.UnknownLength = true
	clientConfig.ContentTypes = []string{"text/plain"}
	img, err = p.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	require.Error(t, err)
}
