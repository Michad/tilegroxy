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

package providers

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_CompositeMVT(t *testing.T) {
	assert.Equal(t, pkg.DataTypeMVT, CompositeMVTRegistration{}.DataType(CompositeMVTConfig{}))
}

// CompositeMVT holds its children directly, outside any layer, so they're unreachable through
// LayerGroup.Close unless CompositeMVT forwards to them itself.
func Test_CompositeMVTCloseClosesChildProviders(t *testing.T) {
	p1 := &closableProvider{}
	p2 := &closableProvider{}
	// ConstructProvider always wraps, so one child is wrapped here to match what production
	// actually builds: the close has to survive both hops, not just the forwarding one
	c := &CompositeMVT{providers: []layer.Provider{layer.ProviderWrapper{Name: "child", Provider: p1}, p2}}

	require.NoError(t, c.Close(context.Background()))

	assert.True(t, p1.closed)
	assert.True(t, p2.closed)
}

func Test_Composite_ExecuteStatic(t *testing.T) {
	provConfig := map[string]interface{}{
		"name":  "static",
		"image": "embedded:box.mvt",
	}

	c, err := CompositeMVTRegistration{}.Initialize(CompositeMVTConfig{Providers: []map[string]interface{}{provConfig, provConfig}}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.NotNil(t, c)
	require.NoError(t, err)

	pc, err := c.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := c.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	assert.NotNil(t, img)
	require.NoError(t, err)

	imgExp, err := images.GetStaticImage("embedded:box.mvt")
	require.NoError(t, err)

	assert.Len(t, img.Content, len(*imgExp)*2)
}
