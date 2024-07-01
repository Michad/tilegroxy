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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/stretchr/testify/assert"
)

// TODO: change this to something less likely to change
const testTemplate = "https://tigerweb.geo.census.gov/arcgis/services/TIGERweb/tigerWMS_PhysicalFeatures/MapServer/WMSServer?VERSION=1.3.0&SERVICE=WMS&REQUEST=GetMap&LAYERS=19&STYLES=&CRS=$srs&BBOX=$xmin,$ymin,$xmax,$ymax&WIDTH=$width&HEIGHT=$height&FORMAT=image/png"

func Test_UrlTemplateValidate(t *testing.T) {
	p, err := ConstructUrlTemplate(UrlTemplateConfig{}, &config.ClientConfig{}, &testErrMessages)

	assert.Nil(t, p)
	assert.Error(t, err)
}
func Test_UrlTemplateExecute(t *testing.T) {
	p, err := ConstructUrlTemplate(UrlTemplateConfig{Template: testTemplate}, &config.ClientConfig{StatusCodes: []int{200}, MaxLength: 2000, ContentTypes: []string{"image/png"}, UnknownLength: true}, &testErrMessages)

	assert.NotNil(t, p)
	assert.NoError(t, err)

	pc, err := p.PreAuth(internal.BackgroundContext(), ProviderContext{})
	assert.NotNil(t, pc)
	assert.NoError(t, err)

	img, err := p.GenerateTile(internal.BackgroundContext(), pc, internal.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.NotNil(t, img)
	assert.NoError(t, err)
}

func Test_UrlTemplateConfigOptions(t *testing.T) {
	var clientConfig = config.ClientConfig{StatusCodes: []int{400}, MaxLength: 2000, ContentTypes: []string{"image/png"}, UnknownLength: true}
	p, err := ConstructUrlTemplate(UrlTemplateConfig{Template: testTemplate}, &clientConfig, &testErrMessages)
	assert.NotNil(t, p)
	assert.NoError(t, err)

	img, err := p.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	assert.Error(t, err)

	clientConfig.StatusCodes = []int{200}
	clientConfig.MaxLength = 2
	img, err = p.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	assert.Error(t, err)

	clientConfig.MaxLength = 2000
	clientConfig.UnknownLength = false
	img, err = p.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	assert.Error(t, err)

	clientConfig.UnknownLength = true
	clientConfig.ContentTypes = []string{"text/plain"}
	img, err = p.GenerateTile(internal.BackgroundContext(), ProviderContext{}, internal.TileRequest{LayerName: "layer", Z: 6, X: 10, Y: 10})
	assert.Nil(t, img)
	assert.Error(t, err)
}
