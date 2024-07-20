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

package entities

import (
	"net/http"
	"time"

	"github.com/Michad/tilegroxy/pkg"
)

type Authentication interface {
	CheckAuthentication(req *http.Request, ctx *pkg.RequestContext) bool
}

type Cache interface {
	Lookup(t pkg.TileRequest) (*pkg.Image, error)
	Save(t pkg.TileRequest, img *pkg.Image) error
}

type Provider interface {
	// Performs authentication before tiles are ever generated. The calling code ensures this is only called once at a time and only when needed
	// based on the expiration in entities.ProviderContext and when an AuthError is returned from GenerateTile
	PreAuth(ctx *pkg.RequestContext, providerContext ProviderContext) (ProviderContext, error)
	GenerateTile(ctx *pkg.RequestContext, providerContext ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error)
}

type ProviderContext struct {
	AuthBypass     bool                   //If true, avoids ever calling preauth again
	AuthExpiration time.Time              //When next to trigger preauth
	AuthToken      string                 //The main auth token that comes back from the preauth and is used by the generate method. Details are up to the provider
	Other          map[string]interface{} //A generic holder in cases where a provider needs extra storage - for instance Blend which needs Context for child providers
}

type Secreter interface {
	Lookup(ctx *pkg.RequestContext, key string) (string, error)
}
