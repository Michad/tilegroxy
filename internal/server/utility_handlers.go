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
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

// This file contains various HTTP Handlers used by the server

// Handler Function for returning a No Content response
func handleNoContent(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// Handler for including Values from the Context as attributes in structured logs
type slogContextHandler struct {
	slog.Handler
	keys []string
}

func (h slogContextHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, k := range h.keys {
		r.AddAttrs(slog.Attr{Key: strings.ToLower(k), Value: slog.AnyValue(ctx.Value(k))})
	}

	return h.Handler.Handle(ctx, r)
}

// Handler for injecting our custom RequestContext struct into the request and catching panics
type httpContextHandler struct {
	http.Handler
	errCfg config.ErrorConfig
}

func (h httpContextHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqC := internal.NewRequestContext(req)
	defer func() {
		if err := recover(); err != nil {
			slog.ErrorContext(&reqC, "Unexpected panic: "+fmt.Sprint(err))
			writeError(&reqC, w, &h.errCfg, TypeOfErrorOther, "Unexpected Internal Server Error", "stack", string(debug.Stack()))
		}
	}()

	h.Handler.ServeHTTP(w, req.WithContext(&reqC))
}

// Handler for redirecting HTTP to HTTPS
type httpRedirectHandler struct {
	protoAndHost string
}

func (h httpRedirectHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, h.protoAndHost+req.RequestURI, http.StatusMovedPermanently)
}
