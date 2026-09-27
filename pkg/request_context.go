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
	"slices"
	"time"
)

// Using context.Context in this way to pass along so much information is controversial. Due to the flexibility of the application
// there's a lot of data that might be needed quite deep and there's places we need to pass control to libraries and back, making it
// difficult to preserve all the information we might need deep in a specific provider any other way.

type requestStateKey struct{}

// Deprecated string keys, still answered so existing custom scripts and placeholders keep working.
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

// RequestState holds what tilegroxy tracks about one incoming request, shared by everything handling it.
type RequestState struct {
	// The raw HTTP request that is being processed
	Req *http.Request
	// When the request was received and started being processed
	StartTime time.Time
	// If true, allowed layers should be restricted. Distinguishes being restricted to no layers from being unrestricted
	LimitLayers bool
	// List of layers allowed via auth
	AllowedLayers []string
	// If true, allowed area should be an "Intersects" and if false allowed area should be a "Contains"
	LimitAreaPartial bool
	// If non-null-island then restrict map to a specific area
	AllowedArea Bounds
	// If auth specifies a way to retrieve a user identifier, it's contained here
	UserID string
	// If auth specifies a way to retrieve a tenant identifier, it's contained here
	TenantID string
	// Maps any parameters in the layer name from their key defined in config to the value from the real URL
	LayerPatternMatches map[string]string
	// How many times the request has been forwarded internally via the ref provider, to guard against cycles
	RefDepth int
	// Whether the tile ultimately served for this request came from the layer's cache
	Cached bool

	fields map[string]any
}

// Lookup resolves a string key exactly as ctx.Value and `{ctx.*}` placeholders have always seen it.
func (s *RequestState) Lookup(key string) (any, bool) {
	if v, ok := s.fields[key]; ok {
		return v, true
	}

	switch key {
	case reqKey:
		return s.Req, true
	case startTimeKey:
		return s.StartTime, true
	case limitLayersKey:
		return &s.LimitLayers, true
	case allowedLayersKey:
		return &s.AllowedLayers, true
	case limitAreaPartialKey:
		return &s.LimitAreaPartial, true
	case allowedAreaKey:
		return &s.AllowedArea, true
	case userIDKey:
		return &s.UserID, true
	case tenantIDKey:
		return &s.TenantID, true
	case layerPatternMatchesKey:
		return &s.LayerPatternMatches, true
	case refDepthKey:
		return &s.RefDepth, true
	case cachedKey:
		return &s.Cached, true
	}

	return nil, false
}

type requestContext struct {
	context.Context

	state *RequestState
}

func (c requestContext) Value(key any) any {
	switch k := key.(type) {
	case requestStateKey:
		return c.state
	case string:
		if v, ok := c.state.Lookup(k); ok {
			return v
		}
	}

	return c.Context.Value(key)
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}

	return host
}

func NewRequestContext(req *http.Request) context.Context {
	state := &RequestState{
		Req:                 req,
		StartTime:           time.Now(),
		AllowedLayers:       []string{},
		LayerPatternMatches: map[string]string{},
		fields: map[string]any{
			"uri":          req.RequestURI,
			"path":         req.URL.Path,
			"query":        req.URL.Query(),
			"query-string": req.URL.RawQuery,
			"proto":        req.Proto,
			"ip":           clientIP(req.RemoteAddr),
			"method":       req.Method,
			"host":         req.Host,
		},
	}

	for header, values := range req.Header {
		if len(values) == 1 {
			state.fields[header] = values[0]
		} else {
			state.fields[header] = values
		}
	}

	return requestContext{Context: req.Context(), state: state}
}

// The state of the request being processed, present when ctx descends from NewRequestContext
func RequestStateFromContext(ctx context.Context) (*RequestState, bool) {
	s, ok := ctx.Value(requestStateKey{}).(*RequestState)
	return s, ok && s != nil
}

// Falls back to the deprecated string key so hand-built contexts keep resolving.
func fromContext[T any](ctx context.Context, field func(*RequestState) *T, legacyKey string) (*T, bool) {
	if s, ok := RequestStateFromContext(ctx); ok {
		return field(s), true
	}

	u, ok := ctx.Value(legacyKey).(*T)
	return u, ok
}

// The raw HTTP request that is being processed
func ReqFromContext(ctx context.Context) (*http.Request, bool) {
	if s, ok := RequestStateFromContext(ctx); ok {
		return s.Req, true
	}

	u, ok := ctx.Value(reqKey).(*http.Request)
	return u, ok
}

// When the request was received and started being processed
func StartTimeFromContext(ctx context.Context) (time.Time, bool) {
	if s, ok := RequestStateFromContext(ctx); ok {
		return s.StartTime, true
	}

	u, ok := ctx.Value(startTimeKey).(time.Time)
	return u, ok
}

// If true, allowed layers should be restricted. Used to distinguish between someone being restricted to no layers vs unrestricted
func LimitLayersFromContext(ctx context.Context) (*bool, bool) {
	return fromContext(ctx, func(s *RequestState) *bool { return &s.LimitLayers }, limitLayersKey)
}

// List of layers allowed via auth
func AllowedLayersFromContext(ctx context.Context) (*[]string, bool) {
	return fromContext(ctx, func(s *RequestState) *[]string { return &s.AllowedLayers }, allowedLayersKey)
}

// If non-null-island then restrict map to a specific area
func AllowedAreaFromContext(ctx context.Context) (*Bounds, bool) {
	return fromContext(ctx, func(s *RequestState) *Bounds { return &s.AllowedArea }, allowedAreaKey)
}

// If true, allowed area should be an "Intersects" and if false allowed area should be a "Contains"
func LimitAreaPartialFromContext(ctx context.Context) (*bool, bool) {
	return fromContext(ctx, func(s *RequestState) *bool { return &s.LimitAreaPartial }, limitAreaPartialKey)
}

// If auth specifies a way to retrieve a user identifier, it's contained here
func UserIDFromContext(ctx context.Context) (*string, bool) {
	return fromContext(ctx, func(s *RequestState) *string { return &s.UserID }, userIDKey)
}

// If auth specifies a way to retrieve a tenant identifier, it's contained here
func TenantIDFromContext(ctx context.Context) (*string, bool) {
	return fromContext(ctx, func(s *RequestState) *string { return &s.TenantID }, tenantIDKey)
}

// Maps any parameters in the layer name from their key defined in config to the value from the real URL
func LayerPatternMatchesFromContext(ctx context.Context) (*map[string]string, bool) {
	return fromContext(ctx, func(s *RequestState) *map[string]string { return &s.LayerPatternMatches }, layerPatternMatchesKey)
}

// Tracks how many times a request has been forwarded internally via the ref provider, to guard against cycles
func RefDepthFromContext(ctx context.Context) (*int, bool) {
	return fromContext(ctx, func(s *RequestState) *int { return &s.RefDepth }, refDepthKey)
}

// Whether the tile ultimately served for this request came from the layer's cache
func CachedFromContext(ctx context.Context) (*bool, bool) {
	return fromContext(ctx, func(s *RequestState) *bool { return &s.Cached }, cachedKey)
}

func BackgroundContext() context.Context {
	req, _ := http.NewRequestWithContext(context.Background(), "", "", nil)

	// Override Go's default of setting these to default strings to avoid logging confusion.  Perhaps we shouldn't be setting the HTTP fields for background context at all
	req.Method = ""
	req.Proto = ""

	return NewRequestContext(req)
}

// Sets the identity an offline run (seed, test) acts as, standing in for what auth would set on a
// real request.
func SetIdentity(ctx context.Context, userID, tenantID string) {
	if u, ok := UserIDFromContext(ctx); ok && u != nil {
		*u = userID
	}

	if t, ok := TenantIDFromContext(ctx); ok && t != nil {
		*t = tenantID
	}
}

// Copies identity and authorization limits into a fresh context, such as one for work outliving the request.
func CopyAuthRestrictions(from, to context.Context) {
	src, srcOk := RequestStateFromContext(from)
	dst, dstOk := RequestStateFromContext(to)

	if srcOk && dstOk {
		dst.LimitLayers = src.LimitLayers
		dst.AllowedLayers = slices.Clone(src.AllowedLayers)
		dst.LimitAreaPartial = src.LimitAreaPartial
		dst.AllowedArea = src.AllowedArea
		dst.UserID = src.UserID
		dst.TenantID = src.TenantID

		return
	}

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
