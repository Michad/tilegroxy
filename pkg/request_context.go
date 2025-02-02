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
	"net/http"
	"strings"
	"time"
)

// Using context.Context in this way to pass along so much information is controversial. Due to the flexibility of the application
// there's a lot of data that might be needed quite deep and there's places we need to pass control to libraries and back, making it
// difficult to preserve all the information we might need deep in a specific provider any other way.

//lint:file-ignore SA1029 Want values to be accessible

const reqKey = "req"
const startTimeKey = "startTime"
const limitLayersKey = "limitLayers"
const allowedLayersKey = "allowedLayers"
const limitAreaPartialKey = "limitAreaPartial"
const allowedAreaKey = "allowedArea"
const userIDKey = "user"
const layerPatternMatchesKey = "layerPatternMatches"

func p[A any](val A) *A {
	return &val
}

//nolint:revive,staticcheck // We want values to be accessible
func NewRequestContext(req *http.Request) context.Context {

	ctx := req.Context()
	ctx = context.WithValue(ctx, reqKey, req)
	ctx = context.WithValue(ctx, startTimeKey, time.Now())
	ctx = context.WithValue(ctx, limitLayersKey, p(false))
	ctx = context.WithValue(ctx, allowedLayersKey, &([]string{}))
	ctx = context.WithValue(ctx, limitAreaPartialKey, p(false))
	ctx = context.WithValue(ctx, allowedAreaKey, &Bounds{})
	ctx = context.WithValue(ctx, userIDKey, p(""))
	ctx = context.WithValue(ctx, layerPatternMatchesKey, &map[string]string{})

	ctx = context.WithValue(ctx, "uri", req.RequestURI)
	ctx = context.WithValue(ctx, "path", req.URL.Path)
	ctx = context.WithValue(ctx, "query", req.URL.Query())
	ctx = context.WithValue(ctx, "query-string", req.URL.RawQuery)
	ctx = context.WithValue(ctx, "proto", req.Proto)
	ctx = context.WithValue(ctx, "ip", strings.Split(req.RemoteAddr, ":")[0])
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

// The raw HTTP request that is being processed
func ReqFromContext(ctx context.Context) (*http.Request, bool) {
	u, ok := ctx.Value(reqKey).(*http.Request)
	return u, ok
}

// When the request was received and started being processed
func StartTimeFromContext(ctx context.Context) (time.Time, bool) {
	u, ok := ctx.Value(startTimeKey).(time.Time)
	return u, ok
}

// If true, allowed layers should be restricted. Used to distinguish between someone being restricted to no layers vs unrestricted
func LimitLayersFromContext(ctx context.Context) (*bool, bool) {
	u, ok := ctx.Value(limitLayersKey).(*bool)
	return u, ok
}

// List of layers allowed via auth
func AllowedLayersFromContext(ctx context.Context) (*[]string, bool) {
	u, ok := ctx.Value(allowedLayersKey).(*[]string)
	return u, ok
}

// If non-null-island then restrict map to a specific area
func AllowedAreaFromContext(ctx context.Context) (*Bounds, bool) {
	u, ok := ctx.Value(allowedAreaKey).(*Bounds)
	return u, ok
}

// If true, allowed area should be an "Intersects" and if false allowed area should be a "Contains"
func LimitAreaPartialFromContext(ctx context.Context) (*bool, bool) {
	u, ok := ctx.Value(limitAreaPartialKey).(*bool)
	return u, ok
}

// If auth specifies a way to retrieve a user identifier, it's contained here
func UserIDFromContext(ctx context.Context) (*string, bool) {
	u, ok := ctx.Value(userIDKey).(*string)
	return u, ok
}

// Maps any parameters in the layer name from their key defined in config to the value from the real URL
func LayerPatternMatchesFromContext(ctx context.Context) (*map[string]string, bool) {
	u, ok := ctx.Value(layerPatternMatchesKey).(*map[string]string)
	return u, ok
}

func BackgroundContext() context.Context {
	req, _ := http.NewRequestWithContext(context.Background(), "", "", nil)
	return NewRequestContext(req)
}
