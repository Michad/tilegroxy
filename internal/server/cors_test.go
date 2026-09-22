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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/internal/authentications"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func corsRequest(t *testing.T, method string, headers map[string]string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, "http://localhost/tiles/l/1/2/3", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return req
}

func Test_CORS_DisabledWritesNothing(t *testing.T) {
	cfg := config.DefaultConfig().Server.CORS

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://example.com"}))

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Vary"))
}

func Test_CORS_EchoesMatchingOrigin(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com", "https://b.example.com"}}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://b.example.com"}))

	assert.Equal(t, "https://b.example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", w.Header().Get("Vary"))
}

func Test_CORS_OriginMatchIsCaseInsensitive(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://A.Example.com"}}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, "https://a.example.com", w.Header().Get("Access-Control-Allow-Origin"))
}

func Test_CORS_NonMatchingOriginStillVaries(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://evil.example.com"}))

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", w.Header().Get("Vary"))
}

func Test_CORS_NoOriginHeader(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, WildcardOrigin: true}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, nil))

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func Test_CORS_Wildcard(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, WildcardOrigin: true}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://anything.example.com"}))

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func Test_CORS_CredentialsAndExposedHeaders(t *testing.T) {
	cfg := config.CORSConfig{
		Enabled:          true,
		Origins:          []string{"https://a.example.com"},
		ExposedHeaders:   []string{"ETag", "Content-Length"},
		AllowCredentials: true,
	}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "ETag, Content-Length", w.Header().Get("Access-Control-Expose-Headers"))
}

func Test_CORS_Preflight(t *testing.T) {
	cfg := config.CORSConfig{
		Enabled: true,
		Origins: []string{"https://a.example.com"},
		Methods: []string{http.MethodGet, http.MethodOptions},
		Headers: []string{"Authorization"},
		MaxAge:  600,
	}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodOptions, map[string]string{
		"Origin":                        "https://a.example.com",
		"Access-Control-Request-Method": http.MethodGet,
	}))

	assert.Equal(t, "GET, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Authorization", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "600", w.Header().Get("Access-Control-Max-Age"))
	assert.Equal(t, []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"}, w.Header().Values("Vary"))
}

func Test_CORS_PreflightHeaderWildcardEchoesRequest(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, WildcardOrigin: true, Headers: []string{"*"}}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodOptions, map[string]string{
		"Origin":                         "https://a.example.com",
		"Access-Control-Request-Method":  http.MethodGet,
		"Access-Control-Request-Headers": "x-custom, authorization",
	}))

	assert.Equal(t, "x-custom, authorization", w.Header().Get("Access-Control-Allow-Headers"))
}

func Test_CORS_PreflightHeaderWildcardWithoutRequestedHeaders(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, WildcardOrigin: true, Headers: []string{"*"}}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodOptions, map[string]string{
		"Origin":                        "https://a.example.com",
		"Access-Control-Request-Method": http.MethodGet,
	}))

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Headers"))
}

// An OPTIONS without Access-Control-Request-Method is an ordinary request, not a preflight
func Test_CORS_OptionsWithoutRequestMethodIsNotPreflight(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, WildcardOrigin: true, Methods: []string{http.MethodGet}, MaxAge: 60}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodOptions, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Methods"))
	assert.Empty(t, w.Header().Get("Access-Control-Max-Age"))
}

func Test_CORS_VaryNotDuplicated(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, WildcardOrigin: true}

	w := httptest.NewRecorder()
	w.Header().Add("Vary", "Accept-Encoding, origin")
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, []string{"Accept-Encoding, origin"}, w.Header().Values("Vary"))
}

func Test_ValidateCORS(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	tests := []struct {
		name    string
		cfg     config.CORSConfig
		wantErr bool
	}{
		{"disabled ignores everything else", config.CORSConfig{WildcardOrigin: true, AllowCredentials: true}, false},
		{"valid", config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}}, false},
		{"no origins is inert, not an error", config.CORSConfig{Enabled: true}, false},
		{"wildcard origin with credentials", config.CORSConfig{Enabled: true, WildcardOrigin: true, AllowCredentials: true}, true},
		{"wildcard header with credentials", config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}, Headers: []string{"*"}, AllowCredentials: true}, true},
		{"wildcard exposed header with credentials", config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}, ExposedHeaders: []string{"*"}, AllowCredentials: true}, true},
		{"wildcard header without credentials", config.CORSConfig{Enabled: true, WildcardOrigin: true, Headers: []string{"*"}}, false},
		{"credentials with explicit origins", config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}, AllowCredentials: true}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCORS(test.cfg, msgs)

			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_CORS_AppliedAsMiddleware(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	corsHandler{inner, cfg}.ServeHTTP(w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, "https://a.example.com", w.Header().Get("Access-Control-Allow-Origin"))
}

// Server.Headers is added by the handler partway through, so CORS has to win on the way out
func Test_CORS_OverwritesStaticHeader(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "https://stale.example.com")
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	corsHandler{inner, cfg}.ServeHTTP(w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, []string{"https://a.example.com"}, w.Header().Values("Access-Control-Allow-Origin"))
}

// A handler that writes a body without calling WriteHeader still has to get the headers
func Test_CORS_AppliedOnImplicitWrite(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, WildcardOrigin: true}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("body"))
	})

	w := httptest.NewRecorder()
	corsHandler{inner, cfg}.ServeHTTP(w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func Test_CORS_OriginMatching(t *testing.T) {
	tests := []struct {
		pattern string
		origin  string
		want    bool
	}{
		{"https://a.example.com", "https://a.example.com", true},
		{"https://a.example.com", "http://a.example.com", false},
		{"a.example.com", "https://a.example.com", true},
		{"a.example.com", "http://a.example.com", true},
		{"https://*.example.com", "https://a.example.com", true},
		{"https://*.example.com", "https://a.b.example.com", false},
		{"https://*.example.com", "https://example.com", false},
		{"https://**.example.com", "https://a.example.com", true},
		{"https://**.example.com", "https://a.b.c.example.com", true},
		{"https://**.example.com", "https://example.com", false},
		{"https://**.example.com", "https://a.example.org", false},
		{"https://a.example.com:8443", "https://a.example.com:8443", true},
		{"https://a.example.com:8443", "https://a.example.com", false},
		{"https://a.example.com", "https://a.example.com:8443", true},
		{"https://A.Example.com", "https://a.example.com", true},
		{"https://*.example.com", "https://a.example.com:8443", true},
		{"http://localhost:3000", "http://localhost:3000", true},
		{"http://localhost:3000", "http://localhost:3001", false},
		{"", "https://a.example.com", false},
		{"https://a.example.com", "https://evil.com", false},
		{"https://**.example.com", "https://evil.com", false},
	}

	for _, test := range tests {
		t.Run(test.pattern+" vs "+test.origin, func(t *testing.T) {
			assert.Equal(t, test.want, originMatchesPattern(test.pattern, test.origin))
		})
	}
}

func Test_CORS_OriginsMatchedAgainstList(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://one.example.com", "https://**.example.org"}}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.b.example.org"}))

	assert.Equal(t, "https://a.b.example.org", w.Header().Get("Access-Control-Allow-Origin"))
}

// Neither WildcardOrigin nor Origins means CORS never triggers
func Test_CORS_NoOriginsConfigured(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true}

	w := httptest.NewRecorder()
	writeCORSHeaders(cfg, w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", w.Header().Get("Vary"))
}

func Test_CORS_IPv6Origin(t *testing.T) {
	assert.True(t, originMatchesPattern("http://[::1]:8080", "http://[::1]:8080"))
	assert.True(t, originMatchesPattern("http://[::1]", "http://[::1]:8080"))
	assert.False(t, originMatchesPattern("http://[::1]:8080", "http://[::1]:9090"))
	assert.False(t, originMatchesPattern("http://[::2]", "http://[::1]"))
}

func Test_CORS_SplitPort(t *testing.T) {
	tests := []struct {
		in       string
		wantHost string
		wantPort string
	}{
		{"example.com", "example.com", ""},
		{"example.com:8443", "example.com", "8443"},
		{"[::1]", "::1", ""},
		{"[::1]:8080", "::1", "8080"},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			host, port := splitPort(test.in)

			assert.Equal(t, test.wantHost, host)
			assert.Equal(t, test.wantPort, port)
		})
	}
}

// A stale grant in Server.Headers must not survive for an origin this config declines
func Test_CORS_ClearsStaticHeaderForDisallowedOrigin(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "https://stale.example.com")
		w.Header().Add("Access-Control-Allow-Credentials", "true")
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	corsHandler{inner, cfg}.ServeHTTP(w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://evil.example.com"}))

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

// Likewise for headers this config simply doesn't set on an allowed origin
func Test_CORS_ClearsStaticHeaderNotReplaced(t *testing.T) {
	cfg := config.CORSConfig{Enabled: true, Origins: []string{"https://a.example.com"}}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Access-Control-Allow-Credentials", "true")
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	corsHandler{inner, cfg}.ServeHTTP(w, corsRequest(t, http.MethodGet, map[string]string{"Origin": "https://a.example.com"}))

	assert.Equal(t, "https://a.example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

// CORS must wrap OUTSIDE the timeout handler. Inside it, a timed-out request loses its CORS
// headers: the timeout path abandons the buffered headers and writes the error to the real
// writer, so the browser reports an opaque CORS failure instead of the 503.
func Test_SetupHandlers_CORSWrapsOutsideTimeout(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.CORS.Enabled = true
	cfg.Server.CORS.Origins = []string{"https://example.com"}
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}
	// Access logging would otherwise wrap outermost and hide which of CORS/timeout is on top
	cfg.Logging.Access.Console = false

	lg, err := layer.ConstructLayerGroup(context.Background(), cfg, cache.NewSingleCacheRegistry(caches.Noop{}), nil, nil)
	require.NoError(t, err)

	rootHandler, err := setupTestRootHandler(&cfg, &entities.Entities{LayerGroup: lg, Auth: authentications.Noop{}})
	require.NoError(t, err)

	// CORS is the outermost wrapper. Anything else means it sank below the timeout handler and a
	// timed-out response would arrive without CORS headers.
	require.IsType(t, corsHandler{}, rootHandler)
}
