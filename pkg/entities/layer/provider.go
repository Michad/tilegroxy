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
	"fmt"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/mitchellh/mapstructure"
)

type Provider interface {
	// Performs authentication before tiles are ever generated. The calling code ensures this is only called once at a time and only when needed
	// based on the expiration in layergroup.ProviderContext and when an AuthError is returned from GenerateTile
	PreAuth(ctx context.Context, providerContext ProviderContext) (ProviderContext, error)
	GenerateTile(ctx context.Context, providerContext ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error)
}

type ProviderContext struct {
	AuthBypass     bool                   // If true, avoids ever calling preauth again
	AuthExpiration time.Time              // When next to trigger preauth
	AuthToken      string                 // The main auth token that comes back from the preauth and is used by the generate method. Details are up to the provider
	Other          map[string]interface{} // A generic holder in cases where a provider needs extra storage - for instance Blend which needs Context for child providers
}

type ProviderRegistration interface {
	Name() string
	Initialize(config any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *LayerGroup, datastores *datastore.DatastoreRegistry) (Provider, error)
	InitializeConfig() any
}

var registrations = make(map[string]ProviderRegistration)

func RegisterProvider(reg ProviderRegistration) {
	registrations[reg.Name()] = reg
}

func RegisteredProvider(name string) (ProviderRegistration, bool) {
	o, ok := registrations[name]
	return o, ok
}

func RegisteredProviderNames() []string {
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

func ConstructProvider(rawConfig map[string]interface{}, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *LayerGroup, datastores *datastore.DatastoreRegistry) (Provider, error) {
	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := RegisteredProvider(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}
			provider, err := reg.Initialize(cfg, clientConfig, errorMessages, layerGroup, datastores)

			if err != nil {
				return nil, err
			}

			return ProviderWrapper{Name: name, Provider: provider}, nil
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.EnumError, "provider.name", nameCoerce, RegisteredProviderNames())
}
