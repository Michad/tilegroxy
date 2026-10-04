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

package layers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// Counts PreAuth calls and always authenticates for a long time
type countingProvider struct {
	preAuthCalls atomic.Int32
}

func (p *countingProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	p.preAuthCalls.Add(1)
	providerContext.AuthExpiration = time.Now().Add(time.Hour)
	return providerContext, nil
}

func (p *countingProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{}, nil
}

func (p *countingProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

// Every goroutine sees the zero AuthExpiration at once, covering both the providerContext race and single PreAuth
func Test_Layer_ConcurrentRenderTileNoCache_NoRace(t *testing.T) {
	provider := &countingProvider{}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	// require.* in a goroutine calls runtime.Goexit, skipping wg.Done and hanging the test
	errs := make(chan error, n)
	for range n {
		go func() {
			defer wg.Done()
			_, err := l.RenderTileNoCache(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, int32(1), provider.preAuthCalls.Load(), "PreAuth should only be called once for concurrent requests on a freshly constructed layer")
}

// Fails the first call so errors.As against the value-typed ProviderAuthError is proven to trigger re-auth
type reauthProvider struct {
	preAuthCalls    atomic.Int32
	generateCalls   atomic.Int32
	failFirstNCalls int32
}

func (p *reauthProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	p.preAuthCalls.Add(1)
	providerContext.AuthExpiration = time.Now().Add(time.Hour)
	return providerContext, nil
}

func (p *reauthProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	call := p.generateCalls.Add(1)
	if call <= p.failFirstNCalls {
		return nil, pkg.ProviderAuthError{Message: "token expired"}
	}
	return &pkg.Image{}, nil
}

func (p *reauthProvider) DataType() config.DataType {
	return config.DataTypeUnknown
}

func Test_Layer_RenderTileNoCache_ReauthsOnProviderAuthError(t *testing.T) {
	provider := &reauthProvider{failFirstNCalls: 1}
	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: provider,
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	img, err := l.RenderTileNoCache(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})

	require.NoError(t, err)
	require.NotNil(t, img)
	require.Equal(t, int32(2), provider.preAuthCalls.Load(), "expected an initial PreAuth plus one re-auth after ProviderAuthError")
	require.Equal(t, int32(2), provider.generateCalls.Load())
}
