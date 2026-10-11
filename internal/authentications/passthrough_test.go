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

package authentications

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type passthroughCall struct {
	method string
	header http.Header
	query  string
}

type passthroughServer struct {
	*httptest.Server
	calls   atomic.Int32
	last    atomic.Pointer[passthroughCall]
	handler atomic.Pointer[http.HandlerFunc]
}

func newPassthroughServer(t *testing.T, handler http.HandlerFunc) *passthroughServer {
	t.Helper()

	s := &passthroughServer{}
	s.handler.Store(&handler)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		s.last.Store(&passthroughCall{method: r.Method, header: r.Header.Clone(), query: r.URL.RawQuery})
		(*s.handler.Load())(w, r)
	}))
	t.Cleanup(s.Close)

	return s
}

func passthroughStatus(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}
}

func passthroughJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func passthroughDeps() authentication.AuthenticationDeps {
	clientConfig := config.DefaultConfig().Client
	clientConfig.ContentTypes = []string{"application/json"}

	return authentication.AuthenticationDeps{
		ErrorMessages: config.DefaultConfig().Error.Messages,
		ClientConfig:  clientConfig,
	}
}

func newTestPassthrough(t *testing.T, cfg map[string]interface{}) *Passthrough {
	t.Helper()

	cfg["name"] = "passthrough"
	auth, err := ConstructAuth(cfg, passthroughDeps())
	require.NoError(t, err)
	t.Cleanup(func() { _ = lifecycle.CloseIfCloser(context.Background(), auth) })

	return auth.(AuthWrapper).Auth.(*Passthrough)
}

func passthroughRequest(t *testing.T, target string, headers map[string]string, cookies map[string]string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, target, nil)
	require.NoError(t, err)

	for k, v := range headers {
		req.Header[k] = []string{v}
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	return req
}

func Test_Passthrough_InitializeValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]interface{}
		err  string
	}{
		{"missing url", map[string]interface{}{"headers": []string{"X-Api-Key"}}, "authentication.url is required"},
		{"bad url scheme", map[string]interface{}{"url": "ftp://example.com", "headers": []string{"X-Api-Key"}}, "authentication.url"},
		{"url without host", map[string]interface{}{"url": "http:///path", "headers": []string{"X-Api-Key"}}, "authentication.url"},
		{"unparseable url", map[string]interface{}{"url": "http://[::1", "headers": []string{"X-Api-Key"}}, "authentication.url"},
		{"bad method", map[string]interface{}{"url": "http://example.com", "method": "PUT", "headers": []string{"X-Api-Key"}}, "authentication.method"},
		{"nothing forwarded", map[string]interface{}{"url": "http://example.com"}, "authentication.headers"},
		{"bad user path", map[string]interface{}{"url": "http://example.com", "headers": []string{"X-Api-Key"}, "userid": "/user["}, "authentication.userid"},
		{"bad tenant path", map[string]interface{}{"url": "http://example.com", "headers": []string{"X-Api-Key"}, "tenantid": "count("}, "authentication.tenantid"},
		{"head with user", map[string]interface{}{"url": "http://example.com", "method": "HEAD", "headers": []string{"X-Api-Key"}, "userid": "/id"}, "authentication.userid"},
		{"head with tenant", map[string]interface{}{"url": "http://example.com", "method": "HEAD", "headers": []string{"X-Api-Key"}, "tenantid": "/id"}, "authentication.tenantid"},
		{"unknown key", map[string]interface{}{"url": "http://example.com", "headers": []string{"X-Api-Key"}, "header": "X-Api-Key"}, "header"},
		{"query param in url", map[string]interface{}{"url": "http://example.com?key=fixed", "queryparams": []string{"key"}}, "authentication.queryparams"},
		{"user without body content type", map[string]interface{}{"url": "http://example.com", "headers": []string{"X-Api-Key"}, "userid": "/id", "client": map[string]interface{}{"contenttypes": []string{"image/png", "not a type"}}}, "authentication.client.contenttypes"},
		{"tenant without body content type", map[string]interface{}{"url": "http://example.com", "headers": []string{"X-Api-Key"}, "tenantid": "/id", "client": map[string]interface{}{"contenttypes": []string{"application/jsonp"}}}, "authentication.client.contenttypes"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg["name"] = "passthrough"
			_, err := ConstructAuth(tc.cfg, passthroughDeps())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.err)
		})
	}
}

func Test_Passthrough_BodyContentTypesAccepted(t *testing.T) {
	for _, ct := range []string{"application/json", "text/xml", "application/problem+json", "application/atom+xml; charset=utf-8"} {
		p := newTestPassthrough(t, map[string]interface{}{"url": "http://example.com", "headers": []string{"X-Api-Key"}, "userid": "/id", "client": map[string]interface{}{"contenttypes": []string{"image/png", ct}}})
		assert.NotNil(t, p, ct)
	}
}

func Test_Passthrough_InitializeDefaults(t *testing.T) {
	p := newTestPassthrough(t, map[string]interface{}{"url": "http://example.com", "cookies": []string{"session"}})

	assert.Equal(t, http.MethodGet, p.Method)
	assert.Equal(t, uint32(passthroughDefaultCacheTTL), p.CacheTTL)
	assert.Nil(t, p.cache)
	assert.Equal(t, passthroughDeps().ClientConfig, p.clientConfig)

	p = newTestPassthrough(t, map[string]interface{}{"url": "http://example.com", "cookies": []string{"session"}, "method": "post", "cachesize": 10})
	assert.Equal(t, http.MethodPost, p.Method)
	assert.NotNil(t, p.cache)
}

func Test_Passthrough_ClientOverrideInheritsUnset(t *testing.T) {
	p := newTestPassthrough(t, map[string]interface{}{
		"url":     "http://example.com",
		"headers": []string{"X-Api-Key"},
		"client":  map[string]interface{}{"timeout": 2, "statuscodes": []int{204}},
	})

	assert.Equal(t, uint(2), p.clientConfig.Timeout)
	assert.Equal(t, []int{204}, p.clientConfig.StatusCodes)
	assert.Equal(t, passthroughDeps().ClientConfig.UserAgent, p.clientConfig.UserAgent)
	assert.Equal(t, []string{"application/json"}, p.clientConfig.ContentTypes)
}

func Test_Passthrough_NothingToForwardSkipsEndpoint(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}, "cookies": []string{"session"}, "queryparams": []string{"key"}})

	req := passthroughRequest(t, "http://tiles/layer/0/0/0?other=1", map[string]string{"X-Other": "a"}, map[string]string{"other": "b"})
	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), req))
	assert.Equal(t, int32(0), srv.calls.Load())
}

func Test_Passthrough_ForwardsOnlyNamedValues(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{
		"url":         srv.URL + "/check?fixed=1",
		"headers":     []string{"X-Api-Key", "X-Missing"},
		"cookies":     []string{"session"},
		"queryparams": []string{"key"},
		"client":      map[string]interface{}{"headers": map[string]string{"X-From-Client": "yes"}},
	})

	req := passthroughRequest(t, "http://tiles/layer/0/0/0?key=a+b&key=c&other=2",
		map[string]string{"X-Api-Key": "secret", "X-Other": "no"},
		map[string]string{"session": "abc", "other": "no"})
	require.True(t, p.CheckAuthentication(pkg.BackgroundContext(), req))

	call := srv.last.Load()
	assert.Equal(t, http.MethodGet, call.method)
	assert.Equal(t, "fixed=1&key=a+b&key=c", call.query)
	assert.Equal(t, []string{"secret"}, call.header["X-Api-Key"])
	assert.Equal(t, "yes", call.header.Get("X-From-Client"))
	assert.Equal(t, passthroughDeps().ClientConfig.UserAgent, call.header.Get("User-Agent"))
	assert.Equal(t, "session=abc", call.header.Get("Cookie"))
	assert.Empty(t, call.header.Get("X-Other"))
	assert.Empty(t, call.header.Get("X-Missing"))
}

func Test_Passthrough_HeaderNamesCanonicalized(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"x-api-key"}})

	require.True(t, p.CheckAuthentication(pkg.BackgroundContext(), passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)))
	assert.Equal(t, []string{"k"}, srv.last.Load().header["X-Api-Key"])
}

func Test_Passthrough_OperatorHeadersWin(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{
		"url":     srv.URL,
		"headers": []string{"Authorization", "User-Agent", "X-Api-Key"},
		"client":  map[string]interface{}{"headers": map[string]string{"Authorization": "Bearer operator"}},
	})

	req := passthroughRequest(t, "http://tiles/a", map[string]string{"Authorization": "Bearer user", "User-Agent": "user-agent", "X-Api-Key": "k"}, nil)
	require.True(t, p.CheckAuthentication(pkg.BackgroundContext(), req))

	call := srv.last.Load()
	assert.Equal(t, []string{"Bearer operator"}, call.header["Authorization"])
	assert.Equal(t, passthroughDeps().ClientConfig.UserAgent, call.header.Get("User-Agent"))
	assert.Equal(t, []string{"k"}, call.header["X-Api-Key"])
}

func Test_Passthrough_ConnectionErrorDoesNotLogForwardedQuery(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL + "/check", "queryparams": []string{"session_id"}})
	srv.Close()

	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), passthroughRequest(t, "http://tiles/a?session_id=supersecret", nil, nil)))
	assert.Contains(t, logs.String(), "/check")
	assert.NotContains(t, logs.String(), "supersecret")
}

func Test_Passthrough_QueryParamsWithoutExistingQuery(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "queryparams": []string{"key"}, "method": "HEAD"})

	require.True(t, p.CheckAuthentication(pkg.BackgroundContext(), passthroughRequest(t, "http://tiles/layer/0/0/0?key=a", nil, nil)))

	call := srv.last.Load()
	assert.Equal(t, http.MethodHead, call.method)
	assert.Equal(t, "key=a", call.query)
}

func Test_Passthrough_StatusDecides(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusUnauthorized))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}, "method": "POST"})
	req := passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)

	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), req))
	assert.Equal(t, http.MethodPost, srv.last.Load().method)

	p = newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}, "client": map[string]interface{}{"statuscodes": []int{401}}})
	assert.True(t, p.CheckAuthentication(pkg.BackgroundContext(), req))
}

func Test_Passthrough_DoesNotFollowRedirects(t *testing.T) {
	login := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	srv := newPassthroughServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, login.URL, http.StatusFound)
	})
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}})

	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)))
	assert.Equal(t, int32(0), login.calls.Load())
}

func Test_Passthrough_ConnectionErrorFails(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}})
	srv.Close()

	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)))
}

func Test_Passthrough_CanceledContextFails(t *testing.T) {
	srv := newPassthroughServer(t, passthroughStatus(http.StatusOK))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}})

	ctx, cancel := context.WithCancel(pkg.BackgroundContext())
	cancel()

	assert.False(t, p.CheckAuthentication(ctx, passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)))
}

func Test_Passthrough_ExtractsIDs(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		user   string
		tenant string
		pass   bool
		ids    [2]string
	}{
		{"json strings", `{"user": {"id": "alice", "org": {"id": "acme"}}}`, "/user/id", "/user/org/id", true, [2]string{"alice", "acme"}},
		{"json numbers", `{"user": {"id": 12345, "org": {"id": 1.5}}}`, "/user/id", "//org/id", true, [2]string{"12345", "1.5"}},
		{"json text node", `{"user": {"id": "alice"}}`, "/user/id/text()", "", true, [2]string{"alice", ""}},
		{"json array element", `{"users": [{"id": "a"}]}`, "/users/*[1]/id", "", true, [2]string{"a", ""}},
		{"json missing", `{"user": {}}`, "/user/id", "/user/org/id", false, [2]string{"", ""}},
		{"json tenant missing", `{"user": {"id": "a"}}`, "/user/id", "/user/org/id", false, [2]string{"", ""}},
		{"json empty string", `{"user": {"id": ""}}`, "/user/id", "", false, [2]string{"", ""}},
		{"json object match", `{"user": {"id": {"x": 1}}}`, "/user/id", "", false, [2]string{"", ""}},
		{"json boolean match", `{"user": {"id": "a", "org": {"id": true}}}`, "/user/id", "/user/org/id", false, [2]string{"", ""}},
		{"json null match", `{"user": {"id": null}}`, "/user/id", "", false, [2]string{"", ""}},
		{"json multiple matches", `{"users": [{"id": "a"}, {"id": "b"}]}`, "//id", "", false, [2]string{"", ""}},
		{"invalid json", `{"user":`, "/user/id", "", false, [2]string{"", ""}},
		{"trailing json", `{"user": {"id": "a"}} {}`, "/user/id", "", false, [2]string{"", ""}},
		{"xml elements", "\ufeff <user><id> alice </id><org><id>acme</id></org></user>", "/user/id", "/user/org/id", true, [2]string{"alice", "acme"}},
		{"xml attribute", `<?xml version="1.0"?><user id="alice" tenant="7"/>`, "/user/@id", "/user/@tenant", true, [2]string{"alice", "7"}},
		{"xml text node", `<user><id>alice</id></user>`, "/user/id/text()", "", true, [2]string{"alice", ""}},
		{"xml missing", `<user/>`, "/user/id", "", false, [2]string{"", ""}},
		{"xml element with children", `<user><id><a>1</a></id></user>`, "/user/id", "", false, [2]string{"", ""}},
		{"xml multiple matches", `<users><id>a</id><id>b</id></users>`, "//id", "", false, [2]string{"", ""}},
		{"xml root match", `<user/>`, "/", "", false, [2]string{"", ""}},
		{"invalid xml", `<user><id>a</user>`, "/user/id", "", false, [2]string{"", ""}},
		{"string function", `<user><id>alice</id></user>`, "concat('u-', /user/id)", "", true, [2]string{"u-alice", ""}},
		{"number function", `{"ids": [1, 2, 3]}`, "count(/ids/*)", "", true, [2]string{"3", ""}},
		{"nan function", `{"user": {}}`, "number(/user/id)", "", false, [2]string{"", ""}},
		{"boolean function", `{"user": {}}`, "boolean(/user)", "", false, [2]string{"", ""}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newPassthroughServer(t, passthroughJSON(tc.body))
			cfg := map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}, "userid": tc.user}
			if tc.tenant != "" {
				cfg["tenantid"] = tc.tenant
			}
			p := newTestPassthrough(t, cfg)

			ctx := pkg.BackgroundContext()
			assert.Equal(t, tc.pass, p.CheckAuthentication(ctx, passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)))

			userID, _ := pkg.UserIDFromContext(ctx)
			tenantID, _ := pkg.TenantIDFromContext(ctx)
			assert.Equal(t, [2]string{*userID, *tenantID}, tc.ids)
		})
	}
}

func Test_Passthrough_BodyIgnoredWithoutIDs(t *testing.T) {
	srv := newPassthroughServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	})
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}})

	assert.True(t, p.CheckAuthentication(pkg.BackgroundContext(), passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)))
}

func Test_Passthrough_BodyLimits(t *testing.T) {
	big := `{"id": "` + string(make([]byte, 100)) + `"}`

	tests := []struct {
		name    string
		handler http.HandlerFunc
		client  map[string]interface{}
		pass    bool
	}{
		{"wrong content type", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(`{"id": "a"}`))
		}, nil, false},
		{"too long", passthroughJSON(big), map[string]interface{}{"maxlength": 50}, false},
		{"unknown length disallowed", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(`{"id": "a"}`))
		}, nil, false},
		{"unknown length allowed", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(`{"id": "a"}`))
		}, map[string]interface{}{"unknownlength": true}, true},
		{"unknown length too long", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(big))
		}, map[string]interface{}{"unknownlength": true, "maxlength": 50}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newPassthroughServer(t, tc.handler)
			cfg := map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}, "userid": "/id"}
			if tc.client != nil {
				cfg["client"] = tc.client
			}
			p := newTestPassthrough(t, cfg)

			assert.Equal(t, tc.pass, p.CheckAuthentication(pkg.BackgroundContext(), passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, nil)))
		})
	}
}

func Test_Passthrough_CachesResultsButNotErrors(t *testing.T) {
	srv := newPassthroughServer(t, passthroughJSON(`{"id": "alice"}`))
	p := newTestPassthrough(t, map[string]interface{}{"url": srv.URL, "headers": []string{"X-Api-Key"}, "cookies": []string{"session"}, "userid": "/id", "cachesize": 100})

	good := passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "k"}, map[string]string{"session": "s"})
	for range 3 {
		ctx := pkg.BackgroundContext()
		assert.True(t, p.CheckAuthentication(ctx, good))
		userID, _ := pkg.UserIDFromContext(ctx)
		assert.Equal(t, "alice", *userID)
	}
	assert.Equal(t, int32(1), srv.calls.Load())

	// Same values under different names must not share a cache entry
	swapped := passthroughRequest(t, "http://tiles/a", nil, map[string]string{"session": "k"})
	assert.True(t, p.CheckAuthentication(pkg.BackgroundContext(), swapped))
	assert.Equal(t, int32(2), srv.calls.Load())

	rejected := passthroughStatus(http.StatusForbidden)
	srv.handler.Store(&rejected)
	denied := passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "bad"}, nil)
	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), denied))
	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), denied))
	assert.Equal(t, int32(3), srv.calls.Load())

	broken := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
	})
	srv.handler.Store(&broken)
	failing := passthroughRequest(t, "http://tiles/a", map[string]string{"X-Api-Key": "err"}, nil)
	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), failing))
	assert.False(t, p.CheckAuthentication(pkg.BackgroundContext(), failing))
	assert.Equal(t, int32(5), srv.calls.Load())
}
