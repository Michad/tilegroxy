// Copyright 2024 Michael Davis
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

package pkg

import (
	"context"
	"net"
	"net/http"
	"time"
)

// Passing this much through context.Context is controversial, but data is needed deep in providers across library boundaries

//lint:file-ignore SA1029 Want values to be accessible

const reqKey = "req"
const startTimeKey = "startTime"
const limitLayersKey = "limitLayers"
const allowedLayersKey = "allowedLayers"
const limitAreaPartialKey = "limitAreaPartial"
const allowedAreaKey = "allowedArea"
const userIDKey = "user"
const tenantIDKey = "tenant"
const layerPatternMatchesKey = "layerPatternMatches"
const refDepthKey = "refDepth"
const cachedKey = "cached"

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}

	return host
}

//nolint:revive,staticcheck // We want values to be accessible
func NewRequestContext(req *http.Request) context.Context {

	ctx := req.Context()
	ctx = context.WithValue(ctx, reqKey, req)
	ctx = context.WithValue(ctx, startTimeKey, time.Now())
	ctx = context.WithValue(ctx, limitLayersKey, new(false))
	ctx = context.WithValue(ctx, allowedLayersKey, &([]string{}))
	ctx = context.WithValue(ctx, limitAreaPartialKey, new(false))
	ctx = context.WithValue(ctx, allowedAreaKey, &Bounds{})
	ctx = context.WithValue(ctx, userIDKey, new(""))
	ctx = context.WithValue(ctx, tenantIDKey, new(""))
	ctx = context.WithValue(ctx, layerPatternMatchesKey, &map[string]string{})
	ctx = context.WithValue(ctx, refDepthKey, new(0))
	ctx = context.WithValue(ctx, cachedKey, new(false))

	ctx = context.WithValue(ctx, "uri", req.RequestURI)
	ctx = context.WithValue(ctx, "path", req.URL.Path)
	ctx = context.WithValue(ctx, "query", req.URL.Query())
	ctx = context.WithValue(ctx, "query-string", req.URL.RawQuery)
	ctx = context.WithValue(ctx, "proto", req.Proto)
	ctx = context.WithValue(ctx, "ip", clientIP(req.RemoteAddr))
	ctx = context.WithValue(ctx, "method", req.Method)
	ctx = context.WithValue(ctx, "host", req.Host)

	for header, values := range req.Header {
		if len(values) == 1 {
			ctx = context.WithValue(ctx, header, values[0])
		} else {
			ctx = context.WithValue(ctx, header, values)
		}
	}

	return ctx
}

func ReqFromContext(ctx context.Context) (*http.Request, bool) {
	u, ok := ctx.Value(reqKey).(*http.Request)
	return u, ok
}

// When the request was received
func StartTimeFromContext(ctx context.Context) (time.Time, bool) {
	u, ok := ctx.Value(startTimeKey).(time.Time)
	return u, ok
}

// Distinguishes being restricted to no layers from being unrestricted
func LimitLayersFromContext(ctx context.Context) (*bool, bool) {
	u, ok := ctx.Value(limitLayersKey).(*bool)
	return u, ok
}

// Layers allowed via auth
func AllowedLayersFromContext(ctx context.Context) (*[]string, bool) {
	u, ok := ctx.Value(allowedLayersKey).(*[]string)
	return u, ok
}

// Restricts the map to an area unless null island
func AllowedAreaFromContext(ctx context.Context) (*Bounds, bool) {
	u, ok := ctx.Value(allowedAreaKey).(*Bounds)
	return u, ok
}

// True means the allowed area is checked with Intersects, false with Contains
func LimitAreaPartialFromContext(ctx context.Context) (*bool, bool) {
	u, ok := ctx.Value(limitAreaPartialKey).(*bool)
	return u, ok
}

// Set when auth can identify the user
func UserIDFromContext(ctx context.Context) (*string, bool) {
	u, ok := ctx.Value(userIDKey).(*string)
	return u, ok
}

// Set when auth can identify the tenant
func TenantIDFromContext(ctx context.Context) (*string, bool) {
	u, ok := ctx.Value(tenantIDKey).(*string)
	return u, ok
}

// Maps each layer name parameter's configured key to its value from the URL
func LayerPatternMatchesFromContext(ctx context.Context) (*map[string]string, bool) {
	u, ok := ctx.Value(layerPatternMatchesKey).(*map[string]string)
	return u, ok
}

// Internal forwards via the ref provider, to guard against cycles
func RefDepthFromContext(ctx context.Context) (*int, bool) {
	u, ok := ctx.Value(refDepthKey).(*int)
	return u, ok
}

// Whether the served tile came from the layer's cache
func CachedFromContext(ctx context.Context) (*bool, bool) {
	u, ok := ctx.Value(cachedKey).(*bool)
	return u, ok
}

func BackgroundContext() context.Context {
	req, _ := http.NewRequestWithContext(context.Background(), "", "", nil)

	// Go's default strings would confuse logs. Perhaps background contexts shouldn't set HTTP fields at all
	req.Method = ""
	req.Proto = ""

	return NewRequestContext(req)
}

// Identity an offline run (seed, test) acts as, standing in for what auth sets on a real request
func SetIdentity(ctx context.Context, userID, tenantID string) {
	if u, ok := UserIDFromContext(ctx); ok && u != nil {
		*u = userID
	}

	if t, ok := TenantIDFromContext(ctx); ok && t != nil {
		*t = tenantID
	}
}

func CopyAuthRestrictions(from, to context.Context) {
	copyPtr(from, to, LimitLayersFromContext)
	copyPtr(from, to, AllowedLayersFromContext)
	copyPtr(from, to, LimitAreaPartialFromContext)
	copyPtr(from, to, AllowedAreaFromContext)
	copyPtr(from, to, UserIDFromContext)
	copyPtr(from, to, TenantIDFromContext)
}

func copyPtr[A any](from, to context.Context, get func(context.Context) (*A, bool)) {
	src, ok := get(from)
	if !ok || src == nil {
		return
	}

	dst, ok := get(to)
	if !ok || dst == nil {
		return
	}

	*dst = *src
}
