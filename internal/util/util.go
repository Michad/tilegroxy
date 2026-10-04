// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	crand "crypto/rand"
	"encoding/binary"
	"math/rand/v2"
	neturl "net/url"
	"slices"
	"strconv"
	"strings"
)

// Query parameter names whose values are masked before a URL is logged.
var credentialQueryParams = []string{"key", "token", "apikey", "api_key", "access_token", "password", "secret", "signature", "sig"}

// RedactURLForLog strips userinfo and masks credential-bearing query parameter values so a URL can
// be written to logs. It accepts relative URIs as well as absolute URLs. A URL that won't parse is
// replaced entirely, since we can't tell which part of it is sensitive.
//
// Providers that log an outgoing URL should route it through this. A {ctx.*} placeholder resolves
// to a value taken off the incoming request, commonly an Authorization header or an API key, so a
// debug log can otherwise end up holding a live credential.
func RedactURLForLog(rawURL string) string {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return "(unparseable url)"
	}

	if parsed.User != nil {
		parsed.User = neturl.User("redacted")
	}

	query := parsed.Query()
	for name := range query {
		if slices.Contains(credentialQueryParams, strings.ToLower(name)) {
			query.Set(name, "redacted")
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.Redacted()
}

// cond ? a : b
func Ternary[T any](cond bool, a T, b T) T {
	if cond {
		return a
	}
	return b
}

// Generates a random string with alphanumeric characters. Specifics are prone to change. Not guaranteed to have cryptographic security
func RandomString() string {
	const base = 36
	const length = 16

	var i, i2 uint64
	b := make([]byte, length)

	// Try to use sRNG by default because why not
	_, err := crand.Read(b)

	if err != nil {
		// Fallback on v2 rand since better that than a potentially unrecoverable error
		i = rand.Uint64()  // #nosec G404
		i2 = rand.Uint64() // #nosec G404
	} else {
		i = binary.BigEndian.Uint64(b[0:(length / 2)])
		i2 = binary.BigEndian.Uint64(b[(length / 2):length])
	}

	return strconv.FormatUint(i, base) + strconv.FormatUint(i2, base)
}
