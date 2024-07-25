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

package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Michad/tilegroxy/internal/authentications"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configToEntities(cfg config.Config) (*layer.LayerGroup, authentication.Authentication, error) {
	cache, err1 := cache.ConstructCache(cfg.Cache, cfg.Error.Messages)
	auth, err2 := authentication.ConstructAuth(cfg.Authentication, cfg.Error.Messages)
	layerGroup, err3 := layer.ConstructLayerGroup(cfg, cfg.Layers, cache, nil)

	return layerGroup, auth, errors.Join(err1, err2, err3)
}

func Test_TileHandler_AllowedArea(t *testing.T) {
	cfg := config.DefaultConfig()
	mainProvider := make(map[string]interface{})
	mainProvider["name"] = "static"
	mainProvider["color"] = "FFF"
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "main", Provider: mainProvider})
	var auth authentication.Authentication
	var cache cache.Cache
	auth = authentications.Noop{}
	cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(cfg, cfg.Layers, cache, nil)
	require.NoError(t, err)

	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	ctx := pkg.BackgroundContext()
	b, _ := pkg.TileRequest{LayerName: "l", Z: 10, X: 12, Y: 12}.GetBounds()
	ctx.AllowedArea = *b
	req1 := httptest.NewRequest("GET", "http://example.com/tiles/main/10/10/10", nil).WithContext(ctx)
	req1.SetPathValue("layer", "main")
	req1.SetPathValue("z", "10")
	req1.SetPathValue("x", "10")
	req1.SetPathValue("y", "10")

	w1 := httptest.NewRecorder()

	handler.ServeHTTP(w1, req1)

	r1 := w1.Result()
	defer func() { require.NoError(t, r1.Body.Close()) }()
	assert.Equal(t, 401, r1.StatusCode)

	req2 := httptest.NewRequest("GET", "http://example.com/tiles/main/10/12/12", nil).WithContext(ctx)
	req2.SetPathValue("layer", "main")
	req2.SetPathValue("z", "10")
	req2.SetPathValue("x", "12")
	req2.SetPathValue("y", "12")

	w2 := httptest.NewRecorder()

	handler.ServeHTTP(w2, req2)
	r2 := w2.Result()
	defer func() { require.NoError(t, r2.Body.Close()) }()
	assert.Equal(t, 200, r2.StatusCode)
}

func Test_TileHandler_Proxy(t *testing.T) {
	var query string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(200)
		b, _ := images.GetStaticImage("color:FFF")
		_, err := w.Write(*b)
		assert.NoError(t, err)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.Client.ContentTypes = append(cfg.Client.ContentTypes, "text/html; charset=UTF-8")
	cfg.Client.UnknownLength = true
	mainProvider := make(map[string]interface{})
	mainProvider["name"] = "proxy"
	os.Setenv("TEST", "t")
	mainProvider["url"] = ts.URL + "?a={layer.a}&b={ctx.user}&t={env.TEST}&z={z}&y={y}&x={x}"
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "main", Pattern: "main_{a}", Provider: mainProvider, Client: &cfg.Client})
	var auth authentication.Authentication
	var cache cache.Cache
	auth = authentications.Noop{}
	cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(cfg, cfg.Layers, cache, nil)
	require.NoError(t, err)

	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	ctx := pkg.BackgroundContext()
	ctx.UserIdentifier = "hi"
	req1 := httptest.NewRequest("GET", "http://example.com/tiles/main/10/10/10", nil).WithContext(ctx)
	req1.SetPathValue("layer", "main_test")
	req1.SetPathValue("z", "10")
	req1.SetPathValue("x", "10")
	req1.SetPathValue("y", "10")

	w1 := httptest.NewRecorder()

	handler.ServeHTTP(w1, req1)

	r1 := w1.Result()
	defer func() { require.NoError(t, r1.Body.Close()) }()
	assert.Equal(t, 200, r1.StatusCode)
	assert.Equal(t, "a=test&b=hi&t=t&z=10&y=10&x=10", query)
}

func Test_TileHandler_RefToStatic(t *testing.T) {
	cfg := config.DefaultConfig()
	mainProvider := make(map[string]interface{})
	mainProvider["name"] = "static"
	mainProvider["color"] = "FFF"
	refProvider := make(map[string]interface{})
	refProvider["name"] = "ref"
	refProvider["layer"] = "main_white"
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "main", Pattern: "main_{something}", Provider: mainProvider})
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "ref", Pattern: "test", Provider: refProvider})
	var auth authentication.Authentication
	var cache cache.Cache
	auth = authentications.Noop{}
	cache = caches.Noop{}
	lg, err := layer.ConstructLayerGroup(cfg, cfg.Layers, cache, nil)
	require.NoError(t, err)

	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://example.com/tiles/test/10/10/10", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "test")
	req1.SetPathValue("z", "10")
	req1.SetPathValue("x", "10")
	req1.SetPathValue("y", "10")

	w1 := httptest.NewRecorder()

	handler.ServeHTTP(w1, req1)
	res1 := w1.Result()
	defer func() { require.NoError(t, res1.Body.Close()) }()

	assert.Equal(t, 200, res1.StatusCode)
	b1, err := io.ReadAll(res1.Body)
	require.NoError(t, err)

	req2 := httptest.NewRequest("GET", "http://example.com/tiles/main_a/10/10/10", nil).WithContext(pkg.BackgroundContext())
	req2.SetPathValue("layer", "main_a")
	req2.SetPathValue("z", "10")
	req2.SetPathValue("x", "10")
	req2.SetPathValue("y", "10")

	w2 := httptest.NewRecorder()

	handler.ServeHTTP(w2, req2)

	res2 := w2.Result()
	defer func() { require.NoError(t, res2.Body.Close()) }()

	assert.Equal(t, 200, res2.StatusCode)
	b2, err := io.ReadAll(res2.Body)
	require.NoError(t, err)

	assert.Equal(t, b2, b1)

	img, _ := images.GetStaticImage("color:FFF")
	assert.Equal(t, *img, b1)
}

func Test_TileHandler_ExecuteCustom(t *testing.T) {
	cfg := config.DefaultConfig()
	mainProvider := make(map[string]interface{})
	mainProvider["name"] = "static"
	mainProvider["color"] = "FFF"
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "color2", SkipCache: true, Provider: mainProvider})
	cfg.Layers = append(cfg.Layers, config.LayerConfig{Id: "color", SkipCache: true, Provider: mainProvider})
	auth := make(map[string]interface{})
	auth["name"] = "custom"
	authToken := make(map[string]interface{})
	auth["token"] = authToken
	authToken["header"] = "X-Token"
	auth["script"] = `
    package custom
    import (
    	"os"
    	"time"
    )
    func validate(token string) (bool, time.Time, string, []string) {
    	if string(token) == "hunter2" {
    		return true, time.Now().Add(1 * time.Hour), "user", []string{"color"}
    	}
    	return false, time.Now().Add(1000 * time.Hour), "", []string{}
    }`

	cache := caches.Noop{}
	lg, err := layer.ConstructLayerGroup(cfg, cfg.Layers, cache, nil)
	require.NoError(t, err)

	authO, err := authentication.ConstructAuth(auth, cfg.Error.Messages)
	require.NoError(t, err)

	handler := tileHandler{defaultHandler{config: &cfg, auth: authO, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://localhost:12341/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "color")
	req1.SetPathValue("z", "8")
	req1.SetPathValue("x", "12")
	req1.SetPathValue("y", "32")

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	res1 := w1.Result()
	defer func() { require.NoError(t, res1.Body.Close()) }()
	assert.Equal(t, 401, res1.StatusCode)

	req2 := httptest.NewRequest("GET", "http://localhost:12341/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req2.Header.Add("X-Token", "hunter2")
	req2.SetPathValue("layer", "color")
	req2.SetPathValue("z", "8")
	req2.SetPathValue("x", "12")
	req2.SetPathValue("y", "32")

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	res2 := w2.Result()
	defer func() { require.NoError(t, res2.Body.Close()) }()
	assert.Equal(t, 200, res2.StatusCode)

	req3 := httptest.NewRequest("GET", "http://localhost:12341/tiles/color2/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req3.Header.Add("X-Token", "hunter2")
	req3.SetPathValue("layer", "color2")
	req3.SetPathValue("z", "8")
	req3.SetPathValue("x", "12")
	req3.SetPathValue("y", "32")

	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	res3 := w3.Result()
	defer func() { require.NoError(t, res3.Body.Close()) }()
	assert.Equal(t, 401, res3.StatusCode)
}

func Test_TileHandler_ExecuteJWT(t *testing.T) {
	configRaw := `server:
  port: 12349
Authentication:
  name: jwt
  Algorithm: HS256
  Key: hunter2
  MaxExpiration: 4294967295
  ExpectedAudience: audience
  ExpectedSubject: subject
  ExpectedIssuer: issuer
  ExpectedScope: tile
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`
	cfg, err := config.LoadConfig(configRaw)
	require.NoError(t, err)
	lg, auth, err := configToEntities(cfg)
	require.NoError(t, err)
	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "color")
	req1.SetPathValue("z", "8")
	req1.SetPathValue("x", "12")
	req1.SetPathValue("y", "32")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req1)
	resp1 := w.Result()
	defer func() { require.NoError(t, resp1.Body.Close()) }()

	assert.Equal(t, 401, resp1.StatusCode)

	req2 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req2.SetPathValue("layer", "color")
	req2.SetPathValue("z", "8")
	req2.SetPathValue("x", "12")
	req2.SetPathValue("y", "32")
	req2.Header.Add("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InNvbWV0aGluZyB0aWxlIG90aGVyIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjQyOTQ5NjcyOTV9.6jOBwjsvFcJXGkaleXB-75F6J3CjaQYuRELJPfvOfQE")

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	resp2 := w2.Result()
	defer func() { require.NoError(t, resp2.Body.Close()) }()

	assert.Equal(t, 200, resp2.StatusCode)
}

// Just make sure it starts up and rejects unauth for now. TODO: figure out how to get the key from logs
func Test_TileHandler_ExecuteStaticRandomKey(t *testing.T) {

	configRaw := `server:
  port: 12348
Authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	cfg, err := config.LoadConfig(configRaw)
	require.NoError(t, err)
	lg, auth, err := configToEntities(cfg)
	require.NoError(t, err)
	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "color")
	req1.SetPathValue("z", "8")
	req1.SetPathValue("x", "12")
	req1.SetPathValue("y", "32")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req1)
	resp := w.Result()
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, 401, resp.StatusCode)
}

func Test_TileHandler_ExecuteStatic(t *testing.T) {

	configRaw := `server:
  port: 12347
Authentication:
  name: static key
  key: hunter2
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	cfg, err := config.LoadConfig(configRaw)
	require.NoError(t, err)
	lg, auth, err := configToEntities(cfg)
	require.NoError(t, err)
	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "color")
	req1.SetPathValue("z", "8")
	req1.SetPathValue("x", "12")
	req1.SetPathValue("y", "32")

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	resp1 := w1.Result()
	defer func() { require.NoError(t, resp1.Body.Close()) }()

	assert.Equal(t, 401, resp1.StatusCode)

	req2 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req2.Header.Add("Authorization", "Bearer hunter2")
	req2.SetPathValue("layer", "color")
	req2.SetPathValue("z", "8")
	req2.SetPathValue("x", "12")
	req2.SetPathValue("y", "32")

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	resp2 := w2.Result()
	defer func() { require.NoError(t, resp2.Body.Close()) }()

	assert.Equal(t, 200, resp2.StatusCode)
}

func Test_TileHandler_ExecuteErrorText(t *testing.T) {
	configRaw := `server:
  port: 12346
Error:
  mode: "text"
authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	cfg, err := config.LoadConfig(configRaw)
	require.NoError(t, err)
	lg, auth, err := configToEntities(cfg)
	require.NoError(t, err)
	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "color")
	req1.SetPathValue("z", "8")
	req1.SetPathValue("x", "12")
	req1.SetPathValue("y", "32")

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	resp := w1.Result()
	defer func() { require.NoError(t, resp.Body.Close()) }()
	fmt.Printf("Header %v\n", resp.Header)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "Not authorized", string(body))
	assert.Nil(t, resp.Header["X-Error-Message"])
}

func Test_TileHandler_ExecuteErrorImage(t *testing.T) {
	configRaw := `server:
  port: 12345
Error:
  mode: "image"
authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	cfg, err := config.LoadConfig(configRaw)
	require.NoError(t, err)
	lg, auth, err := configToEntities(cfg)
	require.NoError(t, err)
	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "color")
	req1.SetPathValue("z", "8")
	req1.SetPathValue("x", "12")
	req1.SetPathValue("y", "32")

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	resp := w1.Result()
	defer func() { require.NoError(t, resp.Body.Close()) }()
	fmt.Printf("Header %v\n", resp.Header)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	img, _ := images.GetStaticImage(images.KeyImageUnauthorized)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, *img, body)
	assert.Nil(t, resp.Header["X-Error-Message"])
}

func Test_TileHandler_ExecuteErrorImageHeader(t *testing.T) {
	configRaw := `server:
  port: 12345
Error:
  mode: "image+header"
authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	cfg, err := config.LoadConfig(configRaw)
	require.NoError(t, err)
	lg, auth, err := configToEntities(cfg)
	require.NoError(t, err)
	handler := tileHandler{defaultHandler{config: &cfg, auth: auth, layerGroup: lg}}

	req1 := httptest.NewRequest("GET", "http://localhost:12349/tiles/color/8/12/32", nil).WithContext(pkg.BackgroundContext())
	req1.SetPathValue("layer", "color")
	req1.SetPathValue("z", "8")
	req1.SetPathValue("x", "12")
	req1.SetPathValue("y", "32")

	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	resp := w1.Result()
	defer func() { require.NoError(t, resp.Body.Close()) }()
	fmt.Printf("Header %v\n", resp.Header)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	img, _ := images.GetStaticImage(images.KeyImageUnauthorized)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, *img, body)
	assert.NotNil(t, resp.Header["X-Error-Message"])
}
