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

func NewRequestContext(req *http.Request) RequestContext {
	return RequestContext{req.Context(), req, time.Now(), false, []string{}, Bounds{}, "", make(map[string]string), false}
}

func BackgroundContext() *RequestContext {
	return &RequestContext{context.Background(), nil, time.Time{}, false, []string{}, Bounds{}, "", make(map[string]string), false}
}

// Custom context type. Links back to request so we can pull attrs into the structured log
type RequestContext struct {
	context.Context
	req                 *http.Request
	startTime           time.Time
	LimitLayers         bool
	AllowedLayers       []string
	AllowedArea         Bounds
	UserIdentifier      string
	LayerPatternMatches map[string]string
	SkipCacheSave       bool
}

func (c *RequestContext) Value(keyAny any) any {

	key, ok := keyAny.(string)
	if !ok {
		return nil
	}

	switch key {
	case "elapsed":
		return time.Since(c.startTime).Seconds()
	case "user":
		return c.UserIdentifier
	}

	if c.req == nil {
		return nil
	}

	switch key {
	case "uri":
		return c.req.RequestURI
	case "path":
		return c.req.URL.Path
	case "query":
		return c.req.URL.Query()
	case "proto":
		return c.req.Proto
	case "ip":
		return strings.Split(c.req.RemoteAddr, ":")[0]
	case "method":
		return c.req.Method
	case "host":
		return c.req.Host
	}

	h, hMatch := c.req.Header[key]

	if hMatch && h != nil {
		if len(h) == 1 {
			return h[0]
		}
		return h
	}

	l, lMatch := c.LayerPatternMatches[key]

	if lMatch {
		return l
	}

	return nil
}
