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
	"time"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_CompositeMVT(t *testing.T) {
	assert.Equal(t, config.DataTypeMVT, CompositeMVTRegistration{}.DataType(CompositeMVTConfig{}))
}

func Test_CompositeMVTCloseClosesChildProviders(t *testing.T) {
	p1 := &closableProvider{}
	p2 := &closableProvider{}
	// ConstructProvider always wraps, so the close must survive both hops like production
	c := &CompositeVector{providers: []layer.Provider{layers.ProviderWrapper{Name: "child", Provider: p1}, p2}, contentType: mvtContentType}

	require.NoError(t, layer.CloseProvider(context.Background(), c))

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

// Misbehaves badly enough to unwind through the compositor
type panicProvider struct{}

func (p panicProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return providerContext, nil
}

func (p panicProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	panic("boom")
}

// A child returning (nil, err) used to leave the collector waiting forever, leaking the goroutine and poisoning the tile
func Test_Composite_ChildErrorDoesNotHang(t *testing.T) {
	cases := map[string]layer.Provider{
		"error": &Fail{FailConfig{Message: "child blew up"}},
		"panic": panicProvider{},
	}

	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			good, err := StaticRegistration{}.Initialize(StaticConfig{Image: "embedded:box.mvt"}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
			require.NoError(t, err)

			c := &CompositeVector{providers: []layer.Provider{bad, good}, errorMessages: testErrMessages, contentType: mvtContentType}

			done := make(chan error, 1)
			go func() {
				_, genErr := c.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})
				done <- genErr
			}()

			select {
			case genErr := <-done:
				require.Error(t, genErr)
			case <-time.After(10 * time.Second):
				assert.Fail(t, "GenerateTile blocked on a starved channel")
			}
		})
	}
}

// Both errors must reach the caller
func Test_Composite_AllChildrenFailJoinsErrors(t *testing.T) {
	c := &CompositeVector{providers: []layer.Provider{&Fail{FailConfig{Message: "first"}}, &Fail{FailConfig{Message: "second"}}}, errorMessages: testErrMessages, contentType: mvtContentType}

	img, err := c.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	assert.Nil(t, img)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "first")
	assert.Contains(t, err.Error(), "second")
}
