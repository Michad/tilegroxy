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

package layer

import (
	"context"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

type Provider interface {
	// Runs before tiles are generated. Called one at a time, when ProviderContext expires or GenerateTile returns an AuthError
	PreAuth(ctx context.Context, providerContext ProviderContext) (ProviderContext, error)
	GenerateTile(ctx context.Context, providerContext ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error)
}

type ProviderContext struct {
	AuthBypass     bool                   // If true, avoids ever calling preauth again
	AuthExpiration time.Time              // When next to trigger preauth
	AuthToken      string                 // The main auth token that comes back from the preauth and is used by the generate method. Details are up to the provider
	Other          map[string]interface{} // A generic holder in cases where a provider needs extra storage - for instance Blend which needs Context for child providers
}

// Lets an entity render tiles from other layers
type LayerGroup interface {
	// Includes the layer's cache and permission checks
	RenderTile(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error)
	// Skips the layer's cache
	RenderTileNoCache(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error)
	// Includes names matching a layer pattern
	HasLayer(ctx context.Context, layerName string) bool
	ListLayerIDs() []string
}

// New dependencies are added as fields so the Initialize signature stays stable
type ProviderDeps struct {
	ClientConfig  config.ClientConfig
	ErrorMessages config.ErrorMessages
	// Lets nesting providers reach sibling layers
	LayerGroup LayerGroup
	Datastores datastore.DatastoreRegistry
}

type ProviderRegistration interface {
	Name() string
	Initialize(config any, deps ProviderDeps) (Provider, error)
	InitializeConfig() any
	// Raster or vector. Nesting providers return config.DataTypeUnknown
	DataType(config any) config.DataType
}

// Lets a ProviderRegistration restrict an already built provider to a layer's bounds
type BoundsWrapper interface {
	WrapBounds(inner Provider, bounds pkg.Bounds, deps ProviderDeps) (Provider, error)
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]ProviderRegistration)

func RegisterProvider(reg ProviderRegistration) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[reg.Name()] = reg
}

func RegisteredProvider(name string) (ProviderRegistration, bool) {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	o, ok := registrations[name]
	return o, ok
}

func RegisteredProviderNames() []string {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}
