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
	"bytes"
	"context"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

// timeoutHandler mirrors the shape of the stdlib's http.TimeoutHandler but, on timeout, formats
// the response through writeError like every other error path instead of always sending a
// hardcoded 503 body. This is the only way to make a timeout honor error.mode: the standard
// handler writes its own status and body directly with no hook to override them.
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
		tw.mu.Lock()
		defer tw.mu.Unlock()
		// Nothing written by the inner handler wins after this point; timeoutWriter.Write starts
		// rejecting writes once tw.timedOut is set.
		tw.timedOut = true
		writeError(ctx, w, h.errCfg, pkg.TimeoutError{}, config.DataTypeUnknown)
	}
}

// timeoutWriter buffers the inner handler's response so it can be discarded if the deadline
// fires before the handler finishes, the same trick the stdlib's timeoutWriter uses.
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
