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
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Ternary(t *testing.T) {
	assert.Equal(t, "a", Ternary(true, "a", "b"))
	assert.Equal(t, "b", Ternary(false, "a", "b"))
}

// Provider URLs are templated and a {ctx.*} placeholder resolves to a value off the incoming
// request, so a debug log of the outgoing URL could carry a live credential.
func Test_RedactURLForLog(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary url untouched", "https://example.com/1/2/3.png", "https://example.com/1/2/3.png"},
		{"non-credential params kept", "https://example.com/t?z=1&x=2", "https://example.com/t?x=2&z=1"},
		{"api key masked", "https://example.com/t?key=abc123", "https://example.com/t?key=redacted"},
		{"access token masked", "https://example.com/t?access_token=abc123", "https://example.com/t?access_token=redacted"},
		{"param name case insensitive", "https://example.com/t?ApiKey=abc123", "https://example.com/t?ApiKey=redacted"},
		{"userinfo masked", "https://user:pass@example.com/t", "https://redacted@example.com/t"},
		// The CGI provider logs a relative URI rather than an absolute URL.
		{"relative uri untouched", "/cgi-bin/mapserv?z=1", "/cgi-bin/mapserv?z=1"},
		{"relative uri key masked", "/cgi-bin/mapserv?key=abc123", "/cgi-bin/mapserv?key=redacted"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RedactURLForLog(c.in))
		})
	}

	assert.NotContains(t, RedactURLForLog("https://example.com/t?token=supersecret"), "supersecret")
	assert.Equal(t, "(unparseable url)", RedactURLForLog("http://[::1]bad:99/"))
}
