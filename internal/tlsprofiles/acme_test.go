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

package tlsprofiles

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Michad/tilegroxy/internal/testutil"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/acme/autocert"
)

func ecdsaHello(name string) *tls.ClientHelloInfo {
	return &tls.ClientHelloInfo{
		ServerName:       name,
		SignatureSchemes: []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
		SupportedCurves:  []tls.CurveID{tls.CurveP256},
		CipherSuites:     []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
	}
}

func seedACMECache(t *testing.T, host string) string {
	t.Helper()

	leaf := testutil.NewCA(t, "ca").Issue(t, host)
	cache := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cache, host), slices.Concat(leaf.KeyPEM, leaf.CertPEM), 0o600))

	return cache
}

func Test_ACMEManager_SharedUntilLastRelease(t *testing.T) {
	cache := seedACMECache(t, "tiles.example.com")

	first := AcquireACME([]string{"a.example.com", "tiles.example.com"}, cache)
	second := AcquireACME([]string{"tiles.example.com", "a.example.com"}, cache)
	assert.Same(t, first, second)

	other := AcquireACME([]string{"tiles.example.com"}, cache)
	assert.NotSame(t, first, other)
	other.Release()

	first.Release()

	cert, err := second.GetCertificate(ecdsaHello("tiles.example.com"))
	require.NoError(t, err)
	assert.NotNil(t, cert)

	second.Release()

	replacement := AcquireACME([]string{"a.example.com", "tiles.example.com"}, cache)
	defer replacement.Release()

	assert.NotSame(t, second, replacement)
}

func Test_ACMEManager_InertAfterRelease(t *testing.T) {
	cache := seedACMECache(t, "tiles.example.com")
	m := AcquireACME([]string{"tiles.example.com"}, cache)

	h := m.HTTPHandler(http.NotFoundHandler())
	probe := httptest.NewRequest(http.MethodGet, "/a", nil)
	probe.Host = "tiles.example.com"

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, probe)
	assert.Equal(t, http.StatusNotFound, rw.Code)

	m.Release()

	_, err := m.GetCertificate(ecdsaHello("tiles.example.com"))
	require.ErrorIs(t, err, errACMEClosed)

	ctx := context.Background()
	require.ErrorIs(t, m.manager.Cache.Put(ctx, "k", []byte("v")), errACMEClosed)
	require.ErrorIs(t, m.manager.Cache.Delete(ctx, "k"), errACMEClosed)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1/", nil)
	require.NoError(t, err)

	_, err = m.manager.Client.HTTPClient.Do(req) //nolint:bodyclose // always an error
	require.ErrorIs(t, err, errACMEClosed)
}

func Test_ACMEManager_LiveCacheAndTransport(t *testing.T) {
	m := AcquireACME([]string{"tiles.example.com"}, t.TempDir())
	defer m.Release()

	ctx := context.Background()

	require.NoError(t, m.manager.Cache.Put(ctx, "k", []byte("v")))

	got, err := m.manager.Cache.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), got)
	require.NoError(t, m.manager.Cache.Delete(ctx, "k"))

	_, err = m.manager.Cache.Get(ctx, "k")
	require.ErrorIs(t, err, autocert.ErrCacheMiss)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	defer upstream.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	require.NoError(t, err)

	resp, err := gatedTransport{&m.closed}.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusTeapot, resp.StatusCode)
}

func Test_Registry_ACMEManagerHeldUntilClose(t *testing.T) {
	cache := seedACMECache(t, "tiles.example.com")

	reg, err := Construct(context.Background(), []map[string]any{
		{"name": "acme", "id": "public", "hosts": []string{"tiles.example.com"}, "cache": cache},
	}, nil, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	profile, err := reg.ServerProfile("public")
	require.NoError(t, err)

	acmeProfile, ok := profile.(*ACME)
	require.True(t, ok)

	m := acmeProfile.Manager()
	assert.Same(t, m, acmeProfile.Manager())

	_, err = m.GetCertificate(ecdsaHello("tiles.example.com"))
	require.NoError(t, err)

	require.NoError(t, reg.Close(context.Background()))
	require.NoError(t, reg.Close(context.Background()))
	assert.True(t, m.closed.Load())
}
