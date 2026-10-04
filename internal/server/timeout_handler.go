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
	"bytes"
	"context"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

// A custom version of http.TimeoutHandler
type timeoutHandler struct {
	handler http.Handler
	dt      time.Duration
	errCfg  *config.ErrorConfig
}

func newTimeoutHandler(handler http.Handler, dt time.Duration, errCfg *config.ErrorConfig) http.Handler {
	return &timeoutHandler{handler: handler, dt: dt, errCfg: errCfg}
}

func (h *timeoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.dt)
	defer cancel()

	r = r.WithContext(ctx)
	done := make(chan struct{})
	tw := &timeoutWriter{w: w, h: make(http.Header)}

	panicChan := make(chan any, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				panicChan <- p
			}
		}()
		h.handler.ServeHTTP(tw, r)
		close(done)
	}()

	select {
	case p := <-panicChan:
		panic(p)
	case <-done:
		tw.mu.Lock()
		defer tw.mu.Unlock()
		maps.Copy(w.Header(), tw.h)
		if !tw.wroteHeader {
			tw.code = http.StatusOK
		}
		w.WriteHeader(tw.code)
		_, _ = w.Write(tw.wbuf.Bytes())
	case <-ctx.Done():
		// Inner handler writes are rejected from here on since tw.timedOut is set
		tw.mu.Lock()
		tw.timedOut = true
		tw.mu.Unlock()
		// Outside the lock since a slow client could block the write, and the inner handler would block behind it
		writeError(ctx, w, h.errCfg, pkg.TimeoutError{}, config.DataTypeUnknown)
	}
}

// Buffers the response so it can be discarded if the deadline fires first, as the stdlib's timeoutWriter does
type timeoutWriter struct {
	w    http.ResponseWriter
	h    http.Header
	wbuf bytes.Buffer

	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
	code        int
}

func (tw *timeoutWriter) Header() http.Header { return tw.h }

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}

	if !tw.wroteHeader {
		tw.writeHeaderLocked(http.StatusOK)
	}

	return tw.wbuf.Write(p)
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	tw.writeHeaderLocked(code)
}

func (tw *timeoutWriter) writeHeaderLocked(code int) {
	if tw.timedOut || tw.wroteHeader {
		return
	}

	tw.wroteHeader = true
	tw.code = code
}
