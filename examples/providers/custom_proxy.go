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

// This example implements a simple "proxy" provider but using a custom provider interface.  It can be used in contrast to the normal proxy provider for a quick performance comparison.

// Package must always be custom
package custom

import (
	//The standard library is available for use
	"math/rand"
	"strconv"
	"strings"

	//This contains types and utility functions from the main tilegroxy application. It is required to always be imported
	"tilegroxy/tilegroxy"
)

// This method is responsible for authenticating outgoing requests and returning a token or whatever else is needed. This method is called when needed by the application. A given instance of tilegroxy will only call this method once at a time and then shares the result among threads. However, this is not shared between instances of tilegroxy.
func preAuth(
	//Contextual information about the http request at play
	ctx tilegroxy.Context,
	//The previous ProviderContext. Will have default/empty values the first time this is called.  Included for use-cases where a refreshToken is available
	providerContext tilegroxy.ProviderContext,
	//The parameters included under the provider in the configuration. In this case it will only contain "url"
	params map[string]interface{},
	//The Client configuration including information such as timeout settings and user agent
	clientConfig tilegroxy.ClientConfig,
	//A mapping for localization of error messages
	errorMessages tilegroxy.ErrorMessages,
) (tilegroxy.ProviderContext, error) {
	//Setting Bypass to true will prevent preAuth ever being subsequently called. Set it for cases where you have no need to authenticate
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

// This method is responsible for creating a tile
func generateTile(
	//Contextual information about the http request at play
	ctx tilegroxy.Context,
	//The Authentication Context returned from the previous call to preAuth
	providerContext tilegroxy.ProviderContext,
	//The main input parameters for the request at hand.  Includes LayerName as well as Z, X, and Y tile coordinates
	tileRequest tilegroxy.TileRequest,
	//The parameters included under the provider in the configuration. In this case it will only contain "url"
	params map[string]interface{},
	//The Client configuration including information such as timeout settings and user agent
	clientConfig tilegroxy.ClientConfig,
	//A mapping for localization of error messages
	errorMessages tilegroxy.ErrorMessages,
) (
	//The resulting image. Currently mapped to []byte
	*tilegroxy.Image,
	//An error for cases when images are not returned. Recommended for this to be mutually exclusive with the Image return. Can be any error type but make it tilegroxy.AuthError type to trigger an auth refresh
	error,
) {
	//An example of how to trigger an authentication refresh. Only useful in cases where Bypass is false
	if rand.Float32() < 0.01 {
		return nil, tilegroxy.AuthError{"Induced failure"}
	}

	url := params["url"].(string)

	url = strings.ReplaceAll(url, "{z}", strconv.Itoa(tileRequest.Z))
	url = strings.ReplaceAll(url, "{y}", strconv.Itoa(tileRequest.Y))
	url = strings.ReplaceAll(url, "{x}", strconv.Itoa(tileRequest.X))

	//This method performs a GET call to the specified URL with all the standard configured headers/timeout settings
	//The third parameter is a map containing custom HTTP headers to include, which should be used for Authentication
	//You can also perform HTTP calls via standard go HTTP library for cases where a GET doesn't suffice. It's recommended
	//to use GetTile where possible for consistency and ensure the configured rules are followed
	return tilegroxy.GetTile(ctx, clientConfig, url, make(map[string]string))
}
