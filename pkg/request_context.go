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

//lint:file-ignore SA1029 Want values to be accessible

const reqKey = "req"
const startTimeKey = "startTime"
const limitLayersKey = "limitLayers"
const allowedLayersKey = "allowedLayers"
const allowedAreaKey = "allowedArea"
const userIDKey = "user"
const layerPatternMatchesKey = "layerPatternMatches"
const skipCacheSaveKey = "skipCacheSave"

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
	ctx = context.WithValue(ctx, allowedAreaKey, &Bounds{})
	ctx = context.WithValue(ctx, userIDKey, p(""))
	ctx = context.WithValue(ctx, layerPatternMatchesKey, &map[string]string{})
	ctx = context.WithValue(ctx, skipCacheSaveKey, p(false))

	ctx = context.WithValue(ctx, "uri", req.RequestURI)
	ctx = context.WithValue(ctx, "path", req.URL.Path)
	ctx = context.WithValue(ctx, "query", req.URL.Query())
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

func ReqFromContext(ctx context.Context) (*http.Request, bool) {
	u, ok := ctx.Value(reqKey).(*http.Request)
	return u, ok
}

func StartTimeFromContext(ctx context.Context) (time.Time, bool) {
	u, ok := ctx.Value(startTimeKey).(time.Time)
	return u, ok
}

func LimitLayersFromContext(ctx context.Context) (*bool, bool) {
	u, ok := ctx.Value(limitLayersKey).(*bool)
	return u, ok
}

func AllowedLayersFromContext(ctx context.Context) (*[]string, bool) {
	u, ok := ctx.Value(allowedLayersKey).(*[]string)
	return u, ok
}

func AllowedAreaFromContext(ctx context.Context) (*Bounds, bool) {
	u, ok := ctx.Value(allowedAreaKey).(*Bounds)
	return u, ok
}

func UserIDFromContext(ctx context.Context) (*string, bool) {
	u, ok := ctx.Value(userIDKey).(*string)
	return u, ok
}

func LayerPatternMatchesFromContext(ctx context.Context) (*map[string]string, bool) {
	u, ok := ctx.Value(layerPatternMatchesKey).(*map[string]string)
	return u, ok
}

func SkipCacheSaveFromContext(ctx context.Context) (*bool, bool) {
	u, ok := ctx.Value(skipCacheSaveKey).(*bool)
	return u, ok
}

func BackgroundContext() context.Context {
	req, _ := http.NewRequestWithContext(context.Background(), "", "", nil)
	return NewRequestContext(req)
}
