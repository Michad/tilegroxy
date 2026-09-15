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

package tg

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

type seenIdentity struct {
	user   string
	tenant string
}

var identityMu sync.Mutex
var identitySeen []seenIdentity

type identityProvider struct{}

func (identityProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (identityProvider) GenerateTile(ctx context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	var seen seenIdentity

	if u, ok := pkg.UserIDFromContext(ctx); ok && u != nil {
		seen.user = *u
	}

	if t, ok := pkg.TenantIDFromContext(ctx); ok && t != nil {
		seen.tenant = *t
	}

	identityMu.Lock()
	identitySeen = append(identitySeen, seen)
	identityMu.Unlock()

	return &pkg.Image{Content: []byte{1, 2, 3}, ContentType: "image/png"}, nil
}

type identityRegistration struct{}

func (identityRegistration) Name() string                   { return "identity-provider" }
func (identityRegistration) InitializeConfig() any          { return struct{}{} }
func (identityRegistration) DataType(_ any) config.DataType { return config.DataTypeRaster }
func (identityRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return identityProvider{}, nil
}

func identityConfig() config.Config {
	layer.RegisterProvider(identityRegistration{})

	identityMu.Lock()
	identitySeen = nil
	identityMu.Unlock()

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "identity_layer", Provider: map[string]interface{}{"name": "identity-provider"}},
	}

	return cfg
}

func recordedIdentities() []seenIdentity {
	identityMu.Lock()
	defer identityMu.Unlock()

	return append([]seenIdentity(nil), identitySeen...)
}

// A provider that varies its output per tenant needs the identity the seed runs as, since there's
// no incoming request for auth to derive one from.
func Test_Seed_PassesIdentityToProvider(t *testing.T) {
	cfg := identityConfig()

	var out bytes.Buffer
	err := Seed(&cfg, SeedOptions{
		LayerName: "identity_layer",
		Zoom:      []uint{0},
		Bounds:    pkg.WorldBounds(),
		NumThread: 1,
		UserID:    "seed-user",
		TenantID:  "seed-tenant",
	}, &out)

	require.NoError(t, err)

	seen := recordedIdentities()
	require.NotEmpty(t, seen)

	for _, s := range seen {
		require.Equal(t, "seed-user", s.user)
		require.Equal(t, "seed-tenant", s.tenant)
	}
}

func Test_Seed_WithoutIdentityLeavesItEmpty(t *testing.T) {
	cfg := identityConfig()

	var out bytes.Buffer
	err := Seed(&cfg, SeedOptions{
		LayerName: "identity_layer",
		Zoom:      []uint{0},
		Bounds:    pkg.WorldBounds(),
		NumThread: 1,
	}, &out)

	require.NoError(t, err)

	seen := recordedIdentities()
	require.NotEmpty(t, seen)

	for _, s := range seen {
		require.Empty(t, s.user)
		require.Empty(t, s.tenant)
	}
}

func Test_Test_PassesIdentityToProvider(t *testing.T) {
	cfg := identityConfig()

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{
		Z: 1, X: 0, Y: 0, CoordinatesSet: true,
		NumThread: 1,
		NoCache:   true,
		UserID:    "test-user",
		TenantID:  "test-tenant",
	}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)

	seen := recordedIdentities()
	require.NotEmpty(t, seen)

	for _, s := range seen {
		require.Equal(t, "test-user", s.user)
		require.Equal(t, "test-tenant", s.tenant)
	}
}

// The tenant cache namespaces by tenant ID, so a seed run has to land in the namespace that same
// tenant's requests read from.
func Test_Seed_TenantReachesCacheNamespace(t *testing.T) {
	cacheDir := t.TempDir()

	cfg := identityConfig()
	cfg.Cache = map[string]interface{}{
		"name": "tenant",
		"cache": map[string]interface{}{
			"name": "disk",
			"path": cacheDir,
		},
	}

	var out bytes.Buffer
	err := Seed(&cfg, SeedOptions{
		LayerName: "identity_layer",
		Zoom:      []uint{0},
		Bounds:    pkg.WorldBounds(),
		NumThread: 1,
		TenantID:  "tenant_a",
	}, &out)

	require.NoError(t, err)

	// Cache writes happen on their own goroutine that the seed doesn't wait on.
	var entries []os.DirEntry
	require.Eventually(t, func() bool {
		entries, err = os.ReadDir(cacheDir)
		return err == nil && len(entries) > 0
	}, 5*time.Second, 10*time.Millisecond)

	// The disk cache re-sanitizes the tenant cache's "~" separator into "_" when building filenames.
	for _, entry := range entries {
		require.True(t, strings.HasPrefix(entry.Name(), "tenant_a_identity_layer"), "cached tile %v isn't namespaced to the tenant", entry.Name())
	}
}
