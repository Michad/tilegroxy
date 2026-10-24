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

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type forwardedResult struct {
	remoteAddr string
	ip         string
	fwd        forwardedRequest
	hasFwd     bool
}

func runForwarded(t *testing.T, trusted []string, headers config.ForwardedHeadersConfig, peer string, reqHeaders http.Header) forwardedResult {
	t.Helper()

	cfg := config.ForwardedConfig{Trusted: trusted, Headers: headers}
	prefixes, err := parseTrustedProxies(cfg, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	var got forwardedResult

	h := forwardedHandler{httpContextHandler{http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		got.remoteAddr = req.RemoteAddr
		got.ip, _ = req.Context().Value("ip").(string)
		got.fwd, got.hasFwd = forwardedFromContext(req.Context())
	}), config.DefaultConfig().Error}, prefixes, headers}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = peer
	req.Header = reqHeaders

	h.ServeHTTP(httptest.NewRecorder(), req)

	return got
}

var defaultForwardedHeaders = config.DefaultConfig().Server.Forwarded.Headers

func Test_Forwarded_XForwardedForChain(t *testing.T) {
	trusted := []string{"10.0.0.0/8", "192.0.2.10"}

	cases := map[string]struct {
		xff  []string
		want string
	}{
		"single":                   {[]string{"203.0.113.5"}, "203.0.113.5"},
		"right-most untrusted":     {[]string{"198.51.100.1, 203.0.113.5, 10.1.1.1"}, "203.0.113.5"},
		"across header lines":      {[]string{"198.51.100.1", "203.0.113.5, 192.0.2.10"}, "203.0.113.5"},
		"every hop trusted":        {[]string{"10.9.9.9, 10.1.1.1"}, "10.9.9.9"},
		"with port":                {[]string{"203.0.113.5:4711"}, "203.0.113.5"},
		"ipv6":                     {[]string{"[2001:db8::1]:80"}, "2001:db8::1"},
		"mapped ipv4":              {[]string{"::ffff:203.0.113.5"}, "203.0.113.5"},
		"garbage stops the walk":   {[]string{"203.0.113.5, nonsense, 10.1.1.1"}, "10.0.0.1"},
		"absent uses the peer":     {nil, "10.0.0.1"},
		"left-most spoof is moot":  {[]string{"1.1.1.1, 203.0.113.5"}, "203.0.113.5"},
		"trusted then client only": {[]string{"203.0.113.5, 10.0.0.2"}, "203.0.113.5"},
	}

	for name, c := range cases {
		h := http.Header{}
		for _, v := range c.xff {
			h.Add("X-Forwarded-For", v)
		}

		got := runForwarded(t, trusted, defaultForwardedHeaders, "10.0.0.1:5555", h)

		assert.Equal(t, c.want, got.ip, name)
		assert.True(t, got.hasFwd, name)
	}
}

func Test_Forwarded_SingleAddressHeader(t *testing.T) {
	headers := defaultForwardedHeaders
	headers.For = "CF-Connecting-IP"

	h := http.Header{}
	h.Set("Cf-Connecting-Ip", "203.0.113.5")
	h.Set("X-Forwarded-For", "198.51.100.1")
	assert.Equal(t, "203.0.113.5", runForwarded(t, []string{"10.0.0.0/8"}, headers, "10.0.0.1:1", h).ip)

	h.Set("Cf-Connecting-Ip", "203.0.113.5, 198.51.100.1")
	assert.Equal(t, "10.0.0.1", runForwarded(t, []string{"10.0.0.0/8"}, headers, "10.0.0.1:1", h).ip, "a list isn't a single address")

	h.Set("Cf-Connecting-Ip", "nonsense")
	assert.Equal(t, "10.0.0.1", runForwarded(t, []string{"10.0.0.0/8"}, headers, "10.0.0.1:1", h).ip)

	headers.For = ""
	h.Set("Cf-Connecting-Ip", "203.0.113.5")
	assert.Equal(t, "10.0.0.1", runForwarded(t, []string{"10.0.0.0/8"}, headers, "10.0.0.1:1", h).ip, "disabled")
}

func Test_Forwarded_UntrustedPeerIsIgnored(t *testing.T) {
	h := http.Header{}
	h.Set("X-Forwarded-For", "203.0.113.5")
	h.Set("X-Forwarded-Host", "evil.example.com")

	got := runForwarded(t, []string{"10.0.0.0/8"}, defaultForwardedHeaders, "198.51.100.7:1", h)
	assert.Equal(t, "198.51.100.7", got.ip)
	assert.Equal(t, "198.51.100.7:1", got.remoteAddr)
	assert.False(t, got.hasFwd)

	got = runForwarded(t, []string{"10.0.0.0/8"}, defaultForwardedHeaders, "not an address", h)
	assert.False(t, got.hasFwd)
}

func Test_Forwarded_HostProtoPort(t *testing.T) {
	h := http.Header{}
	h.Set("X-Forwarded-Proto", "HTTPS, http")
	h.Set("X-Forwarded-Host", "tiles.example.com, proxy.internal")
	h.Set("X-Forwarded-Port", "8443")
	h.Set("X-Forwarded-Prefix", "/maps/")

	got := runForwarded(t, []string{"10.0.0.1"}, defaultForwardedHeaders, "10.0.0.1:1", h)
	assert.Equal(t, forwardedRequest{proto: "https", host: "tiles.example.com", port: "8443", prefix: "/maps"}, got.fwd)

	headers := config.ForwardedHeadersConfig{For: "X-Forwarded-For"}
	got = runForwarded(t, []string{"10.0.0.1"}, headers, "10.0.0.1:1", h)
	assert.Equal(t, forwardedRequest{}, got.fwd, "unset headers aren't read")

	headers.Prefix = "X-Script-Name"
	h.Set("X-Script-Name", "/tiles-api")
	got = runForwarded(t, []string{"10.0.0.1"}, headers, "10.0.0.1:1", h)
	assert.Equal(t, forwardedRequest{prefix: "/tiles-api"}, got.fwd)
}

func Test_Forwarded_PortAndPrefixSanitized(t *testing.T) {
	cases := map[string]struct {
		port, prefix string
		want         forwardedRequest
	}{
		"leading zeros":     {"0443", "/a", forwardedRequest{port: "443", prefix: "/a"}},
		"zero":              {"0", "", forwardedRequest{}},
		"out of range":      {"65536", "", forwardedRequest{}},
		"signed":            {"+80", "", forwardedRequest{}},
		"injected host":     {"80@evil.example.com", "", forwardedRequest{}},
		"relative prefix":   {"", ".evil.example.com", forwardedRequest{}},
		"prefix with query": {"", "/maps?x=1", forwardedRequest{}},
		"prefix with space": {"", "/maps x", forwardedRequest{}},
		"prefix with quote": {"", `/maps"`, forwardedRequest{}},
		"prefix backslash":  {"", `/\\evil.example.com`, forwardedRequest{}},
		"prefix non-ascii":  {"", "/mäps", forwardedRequest{}},
		"prefix encoded":    {"", "/m%C3%A4ps/v1", forwardedRequest{prefix: "/m%C3%A4ps/v1"}},
		"prefix is root":    {"", "/", forwardedRequest{}},
	}

	for name, c := range cases {
		h := http.Header{}
		h.Set("X-Forwarded-Port", c.port)
		h.Set("X-Forwarded-Prefix", c.prefix)

		got := runForwarded(t, []string{"10.0.0.1"}, defaultForwardedHeaders, "10.0.0.1:1", h)
		assert.Equal(t, c.want, got.fwd, name)
	}
}

func Test_Forwarded_InvalidTrustedRejected(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Forwarded.Trusted = []string{"10.0.0.0/8", "nope"}

	require.Error(t, ValidateConfig(&cfg))

	cfg.Server.Forwarded.Trusted = []string{" 10.0.0.0/8 ", "::1", "2001:db8::/32"}
	require.NoError(t, ValidateConfig(&cfg))
}

func Test_Forwarded_AccessLogSeesClientIP(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Forwarded.Trusted = []string{"192.0.2.1"}
	cfg.Logging.Access.Console = false

	var seen string

	root, _, err := wrapRootHandler(&cfg, http.NewServeMux())
	require.NoError(t, err)

	fh, ok := root.(forwardedHandler)
	require.True(t, ok, "forwarding is resolved before anything else, access logging included")

	fh.Handler = http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		seen = req.RemoteAddr
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(pkg.BackgroundContext())
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	fh.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "203.0.113.5", seen)

	cfg.Server.Forwarded.Trusted = nil
	root, _, err = wrapRootHandler(&cfg, http.NewServeMux())
	require.NoError(t, err)

	_, ok = root.(forwardedHandler)
	assert.False(t, ok, "nothing to trust, nothing to wrap")
}
