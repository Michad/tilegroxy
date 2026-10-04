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
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_Static(t *testing.T) {
	assert.Equal(t, config.DataTypeRaster, StaticRegistration{}.DataType(StaticConfig{}))
	assert.Equal(t, config.DataTypeRaster, StaticRegistration{}.DataType(StaticConfig{Color: "F00"}))
	assert.Equal(t, config.DataTypeMVT, StaticRegistration{}.DataType(StaticConfig{Image: "embedded:box.mvt"}))
	assert.Equal(t, config.DataTypeMVT, StaticRegistration{}.DataType(StaticConfig{Image: "/tiles/blank.PBF"}))
	assert.Equal(t, config.DataTypeMLT, StaticRegistration{}.DataType(StaticConfig{Image: "embedded:empty.mlt"}))
}

// Vector tiles have to carry their own content type, clients can't sniff them like a PNG.
func Test_Static_ContentType(t *testing.T) {
	tests := map[string]string{
		"embedded:transparent.png": "image/png",
		"embedded:box.mvt":         "application/vnd.mapbox-vector-tile",
		"embedded:box.mlt":         "application/vnd.maplibre-tile",
	}

	for image, contentType := range tests {
		p, err := StaticRegistration{}.Initialize(StaticConfig{Image: image}, layer.ProviderDeps{ErrorMessages: testErrMessages})
		require.NoError(t, err)

		img, err := p.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l"})
		require.NoError(t, err)
		assert.Equal(t, contentType, img.ContentType, image)
	}
}
