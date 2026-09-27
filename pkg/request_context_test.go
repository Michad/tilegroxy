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

package pkg_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CopyAuthRestrictions(t *testing.T) {
	from := pkg.BackgroundContext()
	to := pkg.BackgroundContext()

	limitLayers, ok := pkg.LimitLayersFromContext(from)
	require.True(t, ok)
	*limitLayers = true
	allowedLayers, ok := pkg.AllowedLayersFromContext(from)
	require.True(t, ok)
	*allowedLayers = []string{"a", "b"}
	partial, ok := pkg.LimitAreaPartialFromContext(from)
	require.True(t, ok)
	*partial = true
	area, ok := pkg.AllowedAreaFromContext(from)
	require.True(t, ok)
	*area = pkg.Bounds{South: 1, North: 2, West: 3, East: 4}
	user, ok := pkg.UserIDFromContext(from)
	require.True(t, ok)
	*user = "someone"
	tenant, ok := pkg.TenantIDFromContext(from)
	require.True(t, ok)
	*tenant = "acme-corp"

	pkg.CopyAuthRestrictions(from, to)

	newLimitLayers, _ := pkg.LimitLayersFromContext(to)
	assert.True(t, *newLimitLayers)
	newAllowedLayers, _ := pkg.AllowedLayersFromContext(to)
	assert.Equal(t, []string{"a", "b"}, *newAllowedLayers)
	newPartial, _ := pkg.LimitAreaPartialFromContext(to)
	assert.True(t, *newPartial)
	newArea, _ := pkg.AllowedAreaFromContext(to)
	assert.Equal(t, pkg.Bounds{South: 1, North: 2, West: 3, East: 4}, *newArea)
	newUser, _ := pkg.UserIDFromContext(to)
	assert.Equal(t, "someone", *newUser)
	newTenant, _ := pkg.TenantIDFromContext(to)
	assert.Equal(t, "acme-corp", *newTenant)
}

// Copying must not alias, so a later change to one context can't reach the other.
func Test_CopyAuthRestrictions_DoesNotAlias(t *testing.T) {
	from := pkg.BackgroundContext()
	to := pkg.BackgroundContext()

	pkg.CopyAuthRestrictions(from, to)

	area, _ := pkg.AllowedAreaFromContext(to)
	*area = pkg.Bounds{South: 5, North: 6, West: 7, East: 8}

	fromArea, _ := pkg.AllowedAreaFromContext(from)
	assert.Equal(t, pkg.Bounds{}, *fromArea)
}

// A context missing the auth values entirely, as happens for anything not built by
// NewRequestContext, must be a no-op rather than a panic.
func Test_CopyAuthRestrictions_MissingValues(t *testing.T) {
	to := pkg.BackgroundContext()

	assert.NotPanics(t, func() { pkg.CopyAuthRestrictions(t.Context(), to) })
	assert.NotPanics(t, func() { pkg.CopyAuthRestrictions(to, t.Context()) })
}

// A freshly built request context has an empty, non-nil tenant ID, mirroring UserIDFromContext's
// default so field resolvers can treat "unset" and "empty string" the same way.
func Test_TenantIDFromContext_DefaultsToEmptyString(t *testing.T) {
	ctx := pkg.BackgroundContext()

	tenant, ok := pkg.TenantIDFromContext(ctx)
	require.True(t, ok)
	assert.Empty(t, *tenant)
}

func Test_SetIdentity(t *testing.T) {
	ctx := pkg.BackgroundContext()

	pkg.SetIdentity(ctx, "someone", "acme-corp")

	user, ok := pkg.UserIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "someone", *user)

	tenant, ok := pkg.TenantIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "acme-corp", *tenant)
}

// A library consumer can pass a plain stdlib context, which carries none of the identity pointers.
func Test_SetIdentity_PlainContext(t *testing.T) {
	require.NotPanics(t, func() {
		pkg.SetIdentity(context.Background(), "someone", "acme-corp")
	})
}

func Test_NewRequestContext_IP(t *testing.T) {
	tests := []struct {
		remoteAddr string
		expected   string
	}{
		{"192.0.2.10:54321", "192.0.2.10"},
		{"[2001:db8::1]:54321", "2001:db8::1"},
		{"[::1]:1234", "::1"},
		{"/run/tilegroxy.sock", "/run/tilegroxy.sock"},
		{"", ""},
	}

	for _, test := range tests {
		t.Run(test.remoteAddr, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/tiles/1/2/3", nil)
			require.NoError(t, err)
			req.RemoteAddr = test.remoteAddr

			ctx := pkg.NewRequestContext(req)
			assert.Equal(t, test.expected, ctx.Value("ip"))
		})
	}
}

// Custom scripts and {ctx.*} placeholders read through ctx.Value with the historic string keys, so
// those reads must keep their types and share storage with the typed accessors.
func Test_NewRequestContext_LegacyStringKeys(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/tiles/1/2/3?a=b", nil)
	require.NoError(t, err)
	req.RemoteAddr = "192.0.2.10:1234"
	req.RequestURI = "/tiles/1/2/3?a=b"
	req.Header.Set("User-Agent", "agent")
	req.Header.Add("X-Multi", "one")
	req.Header.Add("X-Multi", "two")

	ctx := pkg.NewRequestContext(req)

	assert.Equal(t, "/tiles/1/2/3?a=b", ctx.Value("uri"))
	assert.Equal(t, "/tiles/1/2/3", ctx.Value("path"))
	assert.Equal(t, url.Values{"a": []string{"b"}}, ctx.Value("query"))
	assert.Equal(t, "a=b", ctx.Value("query-string"))
	assert.Equal(t, "HTTP/1.1", ctx.Value("proto"))
	assert.Equal(t, "192.0.2.10", ctx.Value("ip"))
	assert.Equal(t, http.MethodGet, ctx.Value("method"))
	assert.Equal(t, "example.com", ctx.Value("host"))
	assert.Equal(t, "agent", ctx.Value("User-Agent"))
	assert.Equal(t, []string{"one", "two"}, ctx.Value("X-Multi"))
	assert.Same(t, req, ctx.Value("req"))
	assert.IsType(t, time.Time{}, ctx.Value("startTime"))
	assert.Nil(t, ctx.Value("Not-A-Header"))

	user, ok := pkg.UserIDFromContext(ctx)
	require.True(t, ok)
	*user = "someone"
	assert.Same(t, user, ctx.Value("user"))

	tenant, _ := pkg.TenantIDFromContext(ctx)
	assert.Same(t, tenant, ctx.Value("tenant"))
	limitLayers, _ := pkg.LimitLayersFromContext(ctx)
	assert.Same(t, limitLayers, ctx.Value("limitLayers"))
	allowedLayers, _ := pkg.AllowedLayersFromContext(ctx)
	assert.Same(t, allowedLayers, ctx.Value("allowedLayers"))
	partial, _ := pkg.LimitAreaPartialFromContext(ctx)
	assert.Same(t, partial, ctx.Value("limitAreaPartial"))
	area, _ := pkg.AllowedAreaFromContext(ctx)
	assert.Same(t, area, ctx.Value("allowedArea"))
	matches, _ := pkg.LayerPatternMatchesFromContext(ctx)
	assert.Same(t, matches, ctx.Value("layerPatternMatches"))
	depth, _ := pkg.RefDepthFromContext(ctx)
	assert.Same(t, depth, ctx.Value("refDepth"))
	cached, _ := pkg.CachedFromContext(ctx)
	assert.Same(t, cached, ctx.Value("cached"))
}

type ctxKey string

// Wrapping the request context further must hide neither the request state nor the parent's values.
func Test_NewRequestContext_DerivedContexts(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxKey("outer"), "parent")
	req, err := http.NewRequestWithContext(parent, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(pkg.NewRequestContext(req))
	defer cancel()
	ctx = context.WithValue(ctx, ctxKey("inner"), "child")

	state, ok := pkg.RequestStateFromContext(ctx)
	require.True(t, ok)
	state.TenantID = "acme-corp"

	tenant, _ := pkg.TenantIDFromContext(ctx)
	assert.Equal(t, "acme-corp", *tenant)
	assert.Equal(t, "parent", ctx.Value(ctxKey("outer")))
	assert.Equal(t, "child", ctx.Value(ctxKey("inner")))
	assert.Equal(t, http.MethodGet, ctx.Value("method"))

	reqOut, ok := pkg.ReqFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, req, reqOut)
	_, ok = pkg.StartTimeFromContext(ctx)
	assert.True(t, ok)

	cancel()
	require.Error(t, ctx.Err())
}

func Test_RequestStateFromContext_PlainContext(t *testing.T) {
	_, ok := pkg.RequestStateFromContext(context.Background())
	assert.False(t, ok)
	_, ok = pkg.ReqFromContext(context.Background())
	assert.False(t, ok)
	_, ok = pkg.StartTimeFromContext(context.Background())
	assert.False(t, ok)
	_, ok = pkg.UserIDFromContext(context.Background())
	assert.False(t, ok)
}

// Stands in for a context a library consumer built by hand with the deprecated string keys.
type legacyContext struct {
	context.Context

	values map[string]any
}

func (c legacyContext) Value(key any) any {
	if k, ok := key.(string); ok {
		return c.values[k]
	}

	return c.Context.Value(key)
}

func Test_Accessors_LegacyHandBuiltContext(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	start := time.Now()
	user := "someone"
	limit := true

	ctx := legacyContext{Context: context.Background(), values: map[string]any{
		"req":         req,
		"startTime":   start,
		"user":        &user,
		"limitLayers": &limit,
	}}

	reqOut, ok := pkg.ReqFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, req, reqOut)
	startOut, ok := pkg.StartTimeFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, start, startOut)
	userOut, ok := pkg.UserIDFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, &user, userOut)

	to := pkg.BackgroundContext()
	pkg.CopyAuthRestrictions(ctx, to)

	newUser, _ := pkg.UserIDFromContext(to)
	assert.Equal(t, "someone", *newUser)
	newLimit, _ := pkg.LimitLayersFromContext(to)
	assert.True(t, *newLimit)
}

// The source request may keep changing its own allowed layers after the copy.
func Test_CopyAuthRestrictions_DoesNotAliasAllowedLayers(t *testing.T) {
	from := pkg.BackgroundContext()
	to := pkg.BackgroundContext()

	src, _ := pkg.RequestStateFromContext(from)
	src.AllowedLayers = []string{"a", "b"}

	pkg.CopyAuthRestrictions(from, to)
	src.AllowedLayers[0] = "changed"

	dst, _ := pkg.RequestStateFromContext(to)
	assert.Equal(t, []string{"a", "b"}, dst.AllowedLayers)
}

func Test_RequestState_Lookup(t *testing.T) {
	state, ok := pkg.RequestStateFromContext(pkg.BackgroundContext())
	require.True(t, ok)

	v, ok := state.Lookup("user")
	require.True(t, ok)
	assert.Same(t, &state.UserID, v)

	_, ok = state.Lookup("nothing")
	assert.False(t, ok)
}
