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

//go:build ignore

// A simple proxy implemented as a custom provider, useful for comparing performance against the built-in proxy provider

// Package must always be custom
package custom

import (
	// The standard library is available for use
	"math/rand"
	"strconv"
	"strings"

	// Types and utility functions from tilegroxy. Always required
	"tilegroxy/tilegroxy"
)

// Authenticates outgoing requests. Called one at a time per instance with the result shared across threads, but not across instances
func preAuth(
	// Contextual information about the incoming request
	ctx tilegroxy.Context,
	// The previous ProviderContext, empty on the first call. Useful when a refresh token is available
	providerContext tilegroxy.ProviderContext,
	// Parameters from the provider config block. Here, only "url"
	params map[string]interface{},
	// Client settings such as timeouts and user agent
	clientConfig tilegroxy.ClientConfig,
	// A mapping for localization of error messages
	errorMessages tilegroxy.ErrorMessages,
) (tilegroxy.ProviderContext, error) {
	// AuthBypass stops preAuth from being called again. Use it when no authentication is needed
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

// Creates a tile
func generateTile(
	// Contextual information about the incoming request
	ctx tilegroxy.Context,
	// The ProviderContext returned by the latest preAuth call
	providerContext tilegroxy.ProviderContext,
	// Includes LayerName and the Z, X and Y tile coordinates
	tileRequest tilegroxy.TileRequest,
	// Parameters from the provider config block. Here, only "url"
	params map[string]interface{},
	// Client settings such as timeouts and user agent
	clientConfig tilegroxy.ClientConfig,
	// A mapping for localization of error messages
	errorMessages tilegroxy.ErrorMessages,
) (
	// Currently mapped to []byte
	*tilegroxy.Image,
	// Prefer returning either an image or an error, not both. A tilegroxy.AuthError triggers an auth refresh
	error,
) {
	// Demonstrates triggering an auth refresh. Only useful when AuthBypass is false
	if rand.Float32() < 0.01 {
		return nil, tilegroxy.AuthError{"Induced failure"}
	}

	url := params["url"].(string)

	url = strings.ReplaceAll(url, "{z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{y}", strconv.Itoa(tileRequest.Y))
	url = strings.ReplaceAll(url, "{x}", strconv.Itoa(tileRequest.X))

	// GetTile applies the configured headers and timeouts. The map adds custom headers like auth. Use net/http for non-GET calls
	return tilegroxy.GetTile(ctx, clientConfig, url, make(map[string]string))
}
