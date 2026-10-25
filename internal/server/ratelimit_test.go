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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/internal/authentications"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/internal/layers"
	internalratelimit "github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fails every call, standing in for a store that's down
type downStore struct{}

func (downStore) Get(_ context.Context, _ []ratelimit.RateLimitCounter) ([]int64, error) {
	return nil, errors.New("down")
}

func (downStore) Increment(_ context.Context, _ []ratelimit.RateLimitCounter) error {
	return errors.New("down")
}

type downStoreRegistration struct{}

func (downStoreRegistration) Name() string          { return "servertestdown" }
func (downStoreRegistration) InitializeConfig() any { return struct{}{} }
func (downStoreRegistration) Initialize(_ any, _ ratelimit.RateLimiterDeps) (ratelimit.RateLimiter, error) {
	return downStore{}, nil
}

func init() {
	ratelimit.RegisterRateLimiter(downStoreRegistration{})
}

// Each test passes its own maxsize so the shared memory store doesn't carry counts between tests
func rateLimitServer(t *testing.T, raw string) *httptest.Server {
	t.Helper()

	cfg := loadTestConfig(t, raw)
	require.NoError(t, ValidateConfig(&cfg))

	reg, err := caches.ConstructCacheRegistry(context.Background(), cfg.Cache, cfg.DefaultCache, nil, cache.CacheDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, reg, nil, nil)
	require.NoError(t, err)

	auth, err := authentications.ConstructAuth(cfg.Authentication, authentication.AuthenticationDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	limiter, err := internalratelimit.ConstructLimiter(context.Background(), cfg, nil, ratelimit.RateLimiterDeps{ErrorMessages: cfg.Error.Messages})
	require.NoError(t, err)

	root, err := setupTestRootHandler(&cfg, &entities.Entities{LayerGroup: lg, Auth: auth, Caches: reg, RateLimit: limiter})
	require.NoError(t, err)

	ts := httptest.NewServer(root)
	t.Cleanup(ts.Close)

	return ts
}

func loadTestConfig(t *testing.T, raw string) config.Config {
	t.Helper()

	cfg, err := configload.LoadConfig(raw)
	require.NoError(t, err)

	return cfg
}

type tileResponse struct {
	StatusCode int
	Header     http.Header
}

func getTile(t *testing.T, url string, headers map[string]string) (tileResponse, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	return tileResponse{res.StatusCode, res.Header}, body
}

const rateLimitLayers = `
layers:
  - id: main
    provider: {name: static, color: FFF}
  - id: vector
    datatype: mvt
    provider: {name: static, image: "embedded:box.mvt"}
  - id: free
    skipratelimit: true
    provider: {name: static, color: FFF}
`

func Test_RateLimit_RejectsWith429(t *testing.T) {
	ts := rateLimitServer(t, `
server:
  cors:
    enabled: true
    wildcardorigin: true
  forwarded:
    trusted: [127.0.0.1, "::1"]
ratelimit:
  name: memory
  maxsize: 9101
  policies:
    - id: a
      rules:
        - {segment: [ip], limit: 2, calendar: hour}
`+rateLimitLayers)

	alice := map[string]string{"X-Forwarded-For": "203.0.113.1", "Origin": "https://example.com"}

	for range 2 {
		res, _ := getTile(t, ts.URL+"/tiles/main/1/1/1", alice)
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, `"a.ip.hour";q=2;w=3600`, res.Header.Get("Ratelimit-Policy"))
		assert.NotEmpty(t, res.Header.Get("Ratelimit"))
		assert.Contains(t, res.Header.Get("Access-Control-Expose-Headers"), "RateLimit")
	}

	res, body := getTile(t, ts.URL+"/tiles/main/1/1/1", alice)
	assert.Equal(t, http.StatusTooManyRequests, res.StatusCode)
	assert.NotEmpty(t, res.Header.Get("Retry-After"))
	assert.Contains(t, res.Header.Get("Ratelimit"), "r=0")
	assert.Equal(t, "image/png", res.Header.Get("Content-Type"))
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))

	img, err := images.GetStaticImage(images.KeyImageRateLimited)
	require.NoError(t, err)
	assert.Equal(t, *img, body)

	// The counter is shared across layers and vector layers get an empty tile
	res, _ = getTile(t, ts.URL+"/tiles/vector/1/1/1", alice)
	assert.Equal(t, http.StatusTooManyRequests, res.StatusCode)
	assert.Equal(t, images.MvtContentType, res.Header.Get("Content-Type"))

	res, _ = getTile(t, ts.URL+"/tiles/free/1/1/1", alice)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Empty(t, res.Header.Get("Ratelimit"))

	res, _ = getTile(t, ts.URL+"/tiles/main/1/1/1", map[string]string{"X-Forwarded-For": "203.0.113.2"})
	assert.Equal(t, http.StatusOK, res.StatusCode, "a different client has its own counter")
}

func Test_RateLimit_FailedChecksAreNotLimited(t *testing.T) {
	ts := rateLimitServer(t, `
ratelimit:
  name: memory
  maxsize: 9102
  policies:
    - id: a
      rules:
        - {segment: [], limit: 1, calendar: hour}
`+rateLimitLayers)

	res, _ := getTile(t, ts.URL+"/tiles/main/1/1/1", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)

	// Neither checked nor counted, so these keep their own errors
	res, _ = getTile(t, ts.URL+"/tiles/main/99/1/1", nil)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	res, _ = getTile(t, ts.URL+"/tiles/nope/1/1/1", nil)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)

	res, _ = getTile(t, ts.URL+"/tiles/main/1/1/1", nil)
	assert.Equal(t, http.StatusTooManyRequests, res.StatusCode)
}

func Test_RateLimit_TextModeAndAlwaysOK(t *testing.T) {
	ts := rateLimitServer(t, `
error:
  mode: text
  alwaysok: true
ratelimit:
  name: memory
  maxsize: 9103
  headers: none
  policies:
    - id: a
      rules:
        - {segment: [], limit: 1, calendar: hour}
`+rateLimitLayers)

	res, _ := getTile(t, ts.URL+"/tiles/main/1/1/1", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)

	res, body := getTile(t, ts.URL+"/tiles/main/1/1/1", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "Rate limit exceeded", string(body))
	assert.Empty(t, res.Header.Get("Retry-After"))
}

func Test_RateLimit_StoreDown(t *testing.T) {
	deny := rateLimitServer(t, `
error:
  mode: text
ratelimit:
  name: servertestdown
  onerror: deny
  policies:
    - id: a
      rules:
        - {segment: [], limit: 1, calendar: hour}
`+rateLimitLayers)

	res, body := getTile(t, deny.URL+"/tiles/main/1/1/1", nil)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	assert.Equal(t, "Unexpected server error: rate limit unavailable", string(body))

	allow := rateLimitServer(t, `
ratelimit:
  name: servertestdown
  policies:
    - id: a
      rules:
        - {segment: [], limit: 1, calendar: hour}
`+rateLimitLayers)

	res, _ = getTile(t, allow.URL+"/tiles/main/1/1/1", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Empty(t, res.Header.Get("Ratelimit"))
}

func Test_RateLimit_CORSExposedHeaders(t *testing.T) {
	cfg := loadTestConfig(t, `
server:
  cors:
    enabled: true
ratelimit:
  name: memory
  policies:
    - id: a
`)
	assert.Equal(t, []string{"ETag", "RateLimit", "RateLimit-Policy", "Retry-After"}, corsWithRateLimitHeaders(cfg.Server.CORS, cfg.RateLimit).ExposedHeaders)

	cfg.Server.CORS.ExposedHeaders = []string{"X-Custom"}
	assert.Equal(t, []string{"X-Custom"}, corsWithRateLimitHeaders(cfg.Server.CORS, cfg.RateLimit).ExposedHeaders, "an explicit list is left alone")

	cfg = loadTestConfig(t, `
server:
  cors:
    enabled: true
`)
	assert.Equal(t, []string{"ETag"}, corsWithRateLimitHeaders(cfg.Server.CORS, cfg.RateLimit).ExposedHeaders)
}

func Test_RateLimit_CORSExposedHeadersFollowReload(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.CORS = config.CORSConfig{Enabled: true, WildcardOrigin: true, ExposedHeaders: cfg.Server.CORS.ExposedHeaders}
	cfg.Layers = []config.LayerConfig{staticLayerConfig("main")}

	routes, err := setupHandlers(&cfg, buildTileJSONTestServing(t, cfg).all)
	require.NoError(t, err)

	exposed := func() string {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/main/8/12/32", nil).WithContext(pkg.BackgroundContext())
		req.Header.Set("Origin", "https://a.example.com")

		w := httptest.NewRecorder()
		routes.root.ServeHTTP(w, req)

		return w.Header().Get("Access-Control-Expose-Headers")
	}

	assert.Equal(t, "ETag", exposed())

	nextCfg := cfg
	nextCfg.RateLimit = map[string]interface{}{"name": "memory"}
	require.NoError(t, routes.reload(&nextCfg, buildTileJSONTestServing(t, nextCfg).all))
	assert.Equal(t, "ETag, RateLimit, RateLimit-Policy, Retry-After", exposed())

	require.NoError(t, routes.reload(&cfg, buildTileJSONTestServing(t, cfg).all))
	assert.Equal(t, "ETag", exposed())

	require.NoError(t, routes.registry.closeAll(context.Background()))
	require.NoError(t, routes.closeAccessLog())
}
