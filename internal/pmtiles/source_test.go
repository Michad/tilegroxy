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

package pmtiles

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sourceData = []byte("0123456789")

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "archive.pmtiles")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

func Test_FileSource_ReadRange(t *testing.T) {
	src, err := OpenFile(writeTempFile(t, sourceData))
	require.NoError(t, err)
	defer src.Close()

	got, err := src.ReadRange(context.Background(), 2, 3)
	require.NoError(t, err)
	assert.Equal(t, []byte("234"), got)

	got, err = src.ReadRange(context.Background(), 8, 5)
	require.NoError(t, err)
	assert.Equal(t, []byte("89"), got)
}

func Test_FileSource_ReadRange_TooLarge(t *testing.T) {
	src, err := OpenFile(writeTempFile(t, sourceData))
	require.NoError(t, err)
	defer src.Close()

	_, err = src.ReadRange(context.Background(), 1<<63, 1)
	require.Error(t, err)

	_, err = src.ReadRange(context.Background(), 0, 1<<40)
	require.Error(t, err)
}

func Test_OpenFile_Missing(t *testing.T) {
	_, err := OpenFile(filepath.Join(t.TempDir(), "nope.pmtiles"))

	require.Error(t, err)
}

func rangeServer(t *testing.T, data []byte, seen *http.Header) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = r.Header.Clone()
		}
		http.ServeContent(w, r, "archive.pmtiles", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func testClientConfig(headers map[string]string) config.ClientConfig {
	if headers == nil {
		headers = make(map[string]string)
	}

	if _, ok := headers["X-Client"]; !ok {
		headers["X-Client"] = "c"
	}

	return config.ClientConfig{UserAgent: "tilegroxy-test", MaxLength: 1 << 20, Timeout: 5, Headers: headers}
}

func Test_HTTPSource_ReadRange(t *testing.T) {
	var seen http.Header
	srv := rangeServer(t, sourceData, &seen)
	src := NewHTTPSource(srv.URL, testClientConfig(map[string]string{"X-Provider": "p"}))
	defer src.Close()

	got, err := src.ReadRange(context.Background(), 2, 3)

	require.NoError(t, err)
	assert.Equal(t, []byte("234"), got)
	assert.Equal(t, "bytes=2-4", seen.Get("Range"))
	assert.Equal(t, "tilegroxy-test", seen.Get("User-Agent"))
	assert.Equal(t, "c", seen.Get("X-Client"))
	assert.Equal(t, "p", seen.Get("X-Provider"))
}

func Test_HTTPSource_ReadRange_PastEnd(t *testing.T) {
	srv := rangeServer(t, sourceData, nil)
	src := NewHTTPSource(srv.URL, testClientConfig(nil))

	got, err := src.ReadRange(context.Background(), 8, 100)

	require.NoError(t, err)
	assert.Equal(t, []byte("89"), got)
}

func Test_HTTPSource_ReadRange_ZeroLength(t *testing.T) {
	src := NewHTTPSource("http://127.0.0.1:0", testClientConfig(nil))

	got, err := src.ReadRange(context.Background(), 5, 0)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func Test_HTTPSource_RangeIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sourceData)
	}))
	t.Cleanup(srv.Close)
	src := NewHTTPSource(srv.URL, testClientConfig(nil))

	// Offset 0 but body (10 bytes) longer than the requested range (3 bytes): still ignored.
	_, err := src.ReadRange(context.Background(), 0, 3)

	require.ErrorIs(t, err, errRangeIgnored)
}

func Test_HTTPSource_RangeIgnored_NonZeroOffset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sourceData)
	}))
	t.Cleanup(srv.Close)
	src := NewHTTPSource(srv.URL, testClientConfig(nil))

	_, err := src.ReadRange(context.Background(), 2, 3)

	require.ErrorIs(t, err, errRangeIgnored)
}

func Test_HTTPSource_RangeIgnored_OffsetZeroBodyFits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sourceData)
	}))
	t.Cleanup(srv.Close)
	src := NewHTTPSource(srv.URL, testClientConfig(nil))

	// Accepting this would let Open pass on small archives, then fail every tile read.
	_, err := src.ReadRange(context.Background(), 0, uint64(len(sourceData)))

	require.ErrorIs(t, err, errRangeIgnored)
}

func Test_HTTPSource_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	src := NewHTTPSource(srv.URL, testClientConfig(nil))

	_, err := src.ReadRange(context.Background(), 0, 3)

	var remoteErr pkg.RemoteServerError
	require.ErrorAs(t, err, &remoteErr)
	assert.Equal(t, http.StatusNotFound, remoteErr.StatusCode)
}

func Test_HTTPSource_BodyLongerThanRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(sourceData)
	}))
	t.Cleanup(srv.Close)
	src := NewHTTPSource(srv.URL, testClientConfig(nil))

	_, err := src.ReadRange(context.Background(), 0, 3)

	require.Error(t, err)
}

func Test_HTTPSource_Unreachable(t *testing.T) {
	src := NewHTTPSource("http://127.0.0.1:1", testClientConfig(nil))

	_, err := src.ReadRange(context.Background(), 0, 3)

	require.Error(t, err)
}
