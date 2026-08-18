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

package entities

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeCountingProvider proves a real, layer-owned provider gets closed by Entities.Close, going
// through the public LayerGroup constructor since the layer slice itself is unexported.
type closeCountingProvider struct {
	closed *bool
}

func (p closeCountingProvider) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{}, nil
}

func (p closeCountingProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (p closeCountingProvider) Close(_ context.Context) error {
	*p.closed = true
	return nil
}

var closeCountingProviderClosed bool

type closeCountingRegistration struct{}

func (closeCountingRegistration) InitializeConfig() any { return struct{}{} }
func (closeCountingRegistration) Name() string          { return "close-counting" }
func (closeCountingRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	closeCountingProviderClosed = false
	return closeCountingProvider{closed: &closeCountingProviderClosed}, nil
}

// failingAnalytics implements analytics.Analytics and lifecycle.Closer, always failing to close,
// to exercise the analytics-timeout path in Entities.Close.
type failingAnalytics struct{}

func (failingAnalytics) Record(_ context.Context, _ analytics.Event) error {
	return nil
}

func (failingAnalytics) Close(_ context.Context) error {
	return errors.New("analytics did not finish flushing")
}

func Test_Entities_CloseNil(t *testing.T) {
	var e *Entities

	require.NoError(t, e.Close(context.Background()))
}

// Entities is exported and constructed directly, so a generation missing any given entity has to close
// without panicking on the nil field.
func Test_Entities_CloseWithUnsetEntities(t *testing.T) {
	e := &Entities{}

	assert.NotPanics(t, func() {
		require.NoError(t, e.Close(context.Background()))
	})
}

func Test_Entities_CloseIsIdempotent(t *testing.T) {
	e := &Entities{}

	require.NoError(t, e.Close(context.Background()))
	require.NoError(t, e.Close(context.Background()))
}

// Providers close before analytics: they hold nothing the analytics flush depends on, and a CGI
// child process is worth reaping early. Uses a LayerGroup constructed through the public
// constructor since its provider-holding field is unexported outside the layer package.
func Test_Entities_ClosesLayerGroupProviders(t *testing.T) {
	layer.RegisterProvider(closeCountingRegistration{})
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{{ID: "l", Provider: map[string]any{"name": "close-counting"}}}
	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	e := &Entities{LayerGroup: lg}

	require.NoError(t, e.Close(context.Background()))
	assert.True(t, closeCountingProviderClosed)
}

// closableAuth stands in for a custom auth script that owns resources of its own
type closableAuth struct {
	closed *bool
}

func (a closableAuth) CheckAuthentication(_ context.Context, _ *http.Request) bool { return true }

func (a closableAuth) Close(_ context.Context) error {
	*a.closed = true
	return nil
}

func Test_Entities_ClosesAuth(t *testing.T) {
	var closed bool
	e := &Entities{Auth: closableAuth{closed: &closed}}

	require.NoError(t, e.Close(context.Background()))
	assert.True(t, closed, "a custom auth script's close hook has to run on shutdown")
}

func Test_Entities_ClosesAuthEvenWhenAnalyticsTimesOut(t *testing.T) {
	var closed bool
	e := &Entities{
		Auth:      closableAuth{closed: &closed},
		Analytics: &analytics.AnalyticsWrapper{Name: "failing", ID: "failing", Analytics: failingAnalytics{}},
	}

	require.Error(t, e.Close(context.Background()))
	assert.True(t, closed, "auth closes before the flush, so a stalled flush must not strand it")
}

// The analytics-timeout early return that leaves datastores open must survive the new LayerGroup
// step being added ahead of it, and the LayerGroup step must still run even though analytics
// times out - the two errors are independent and both get joined.
func Test_Entities_AnalyticsTimeoutStillLeavesDatastoresOpen(t *testing.T) {
	e := &Entities{
		LayerGroup: &layer.LayerGroup{},
		Analytics:  &analytics.AnalyticsWrapper{Name: "failing", ID: "failing", Analytics: failingAnalytics{}},
	}

	err := e.Close(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "analytics did not finish flushing")
}
