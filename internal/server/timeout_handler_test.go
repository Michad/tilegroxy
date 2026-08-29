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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_TimeoutHandler_PassesThroughFastRequests(t *testing.T) {
	cfg := config.DefaultConfig()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})

	h := newTimeoutHandler(inner, time.Second, &cfg.Error)

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))

	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, http.StatusTeapot, r.StatusCode)
	b, _ := io.ReadAll(r.Body)
	assert.Equal(t, "ok", string(b))
}

func Test_TimeoutHandler_HonorsErrorMode_Text(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Error.Mode = config.ModeErrorPlainText

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	h := newTimeoutHandler(inner, time.Millisecond, &cfg.Error)

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))

	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, http.StatusServiceUnavailable, r.StatusCode)
	b, _ := io.ReadAll(r.Body)
	assert.Equal(t, cfg.Error.Messages.Timeout, string(b))
}

func Test_TimeoutHandler_HonorsErrorMode_None(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Error.Mode = config.ModeErrorNoError

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	h := newTimeoutHandler(inner, time.Millisecond, &cfg.Error)

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))

	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, http.StatusServiceUnavailable, r.StatusCode)
	b, _ := io.ReadAll(r.Body)
	assert.Empty(t, b)
}

func Test_TimeoutHandler_AlwaysOk(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Error.AlwaysOK = true

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	h := newTimeoutHandler(inner, time.Millisecond, &cfg.Error)

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))

	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, http.StatusOK, r.StatusCode)
}

func Test_TimeoutHandler_DiscardsLateWrites(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Error.Mode = config.ModeErrorPlainText

	writeAttempted := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		_, _ = w.Write([]byte("too late"))
		close(writeAttempted)
	})

	h := newTimeoutHandler(inner, time.Millisecond, &cfg.Error)

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	<-writeAttempted

	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	b, _ := io.ReadAll(r.Body)
	assert.Equal(t, cfg.Error.Messages.Timeout, string(b))
}
