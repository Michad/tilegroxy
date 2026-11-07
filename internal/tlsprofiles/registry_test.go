// Copyright 2026 Michael Davis
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
package tlsprofiles

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/internal/testutil"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSecreter map[string]string

func (s stubSecreter) Lookup(_ context.Context, key string) (string, string, error) {
	return s[key], "", nil
}

func (s stubSecreter) Check(_ context.Context, keys []string) ([]string, error) {
	return make([]string, len(keys)), nil
}

func messages() config.ErrorMessages {
	return config.DefaultConfig().Error.Messages
}

func buildRegistry(t *testing.T, entries ...map[string]any) (*Registry, error) {
	t.Helper()
	return Construct(context.Background(), entries, stubSecreter{}, messages())
}

func mustBuild(t *testing.T, entries ...map[string]any) *Registry {
	t.Helper()

	r, err := buildRegistry(t, entries...)
	require.NoError(t, err)

	return r
}

func Test_Construct_Rejects(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "tiles.example.com")
	otherKey := ca.Issue(t, "other.example.com").KeyPEM

	pemWith := func(extra map[string]any) map[string]any {
		m := map[string]any{"name": "pem", "id": "a"}
		for k, v := range extra {
			m[k] = v
		}

		return m
	}

	tests := map[string]map[string]any{
		"no name":             {"id": "a"},
		"unknown name":        {"name": "nope", "id": "a"},
		"no id":               {"name": "pem"},
		"reserved id":         {"name": "pem", "id": "system"},
		"unknown param":       pemWith(map[string]any{"cheese": "yes"}),
		"bad version":         pemWith(map[string]any{"minversion": "1.1"}),
		"unquoted version":    pemWith(map[string]any{"minversion": 1.3}),
		"cert without key":    pemWith(map[string]any{"certificate": string(leaf.CertPEM)}),
		"key without cert":    pemWith(map[string]any{"key": string(leaf.KeyPEM)}),
		"mismatched pair":     pemWith(map[string]any{"certificate": string(leaf.CertPEM), "key": string(otherKey)}),
		"both cert forms":     pemWith(map[string]any{"certificate": string(leaf.CertPEM), "certificatefile": "x", "key": string(leaf.KeyPEM)}),
		"both key forms":      pemWith(map[string]any{"certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "keyfile": "x"}),
		"both ca forms":       pemWith(map[string]any{"ca": string(ca.CertPEM), "cafile": "x"}),
		"missing file":        pemWith(map[string]any{"cafile": "/does/not/exist"}),
		"empty ca file":       pemWith(map[string]any{"cafile": testutil.WriteTempFile(t, "empty.pem", []byte(" \n"))}),
		"unset env ca":        pemWith(map[string]any{"ca": "env.TEST_TLS_UNSET_VARIABLE"}),
		"invalid ca":          pemWith(map[string]any{"ca": "not pem"}),
		"hosts without cert":  pemWith(map[string]any{"hosts": []string{"tiles.example.com"}}),
		"host not covered":    pemWith(map[string]any{"certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "hosts": []string{"other.example.com"}}),
		"wildcard host":       pemWith(map[string]any{"certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "hosts": []string{"*.example.com"}}),
		"mid wildcard":        pemWith(map[string]any{"allowedhosts": []string{"a.*.example.com"}}),
		"acme without hosts":  {"name": "acme", "id": "a"},
		"acme wildcard":       {"name": "acme", "id": "a", "hosts": []string{"*.example.com"}},
		"acme invalid host":   {"name": "acme", "id": "a", "hosts": []string{"bad host"}},
		"acme ip":             {"name": "acme", "id": "a", "hosts": []string{"10.0.0.1"}},
		"acme pem param":      {"name": "acme", "id": "a", "hosts": []string{"a.example.com"}, "ca": "x"},
		"acme bad minversion": {"name": "acme", "id": "a", "hosts": []string{"a.example.com"}, "minversion": "2"},
		"selfsigned wildcard": {"name": "selfsigned", "id": "a", "hosts": []string{"*.example.com"}},
		"selfsigned bad host": {"name": "selfsigned", "id": "a", "hosts": []string{"bad host"}},
		"selfsigned ca param": {"name": "selfsigned", "id": "a", "ca": "x"},
		"selfsigned version":  {"name": "selfsigned", "id": "a", "minversion": "2"},
	}

	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := buildRegistry(t, entry)
			require.Error(t, err)
		})
	}

	t.Run("duplicate id", func(t *testing.T) {
		_, err := buildRegistry(t, map[string]any{"name": "pem", "id": "a"}, map[string]any{"name": "pem", "id": "a"})
		require.Error(t, err)
	})
}

func Test_Construct_ErrorsDontExposeKey(t *testing.T) {
	leaf := testutil.NewCA(t, "ca").Issue(t, "a.example.com")

	_, err := buildRegistry(t, map[string]any{"name": "pem", "id": "a", "certificate": string(leaf.CertPEM), "key": "SENSITIVE-KEY-TEXT"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SENSITIVE-KEY-TEXT")
}

func Test_Construct_ResolvesEnvAndSecrets(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "a.example.com")

	t.Setenv("TEST_TLS_CERT", string(leaf.CertPEM))

	r, err := Construct(context.Background(), []map[string]any{{
		"Name": "pem", "ID": "a", "certificate": "env.TEST_TLS_CERT", "Key": "secret.key",
	}}, stubSecreter{"key": string(leaf.KeyPEM)}, messages())
	require.NoError(t, err)

	profile, err := r.ServerProfile("a")
	require.NoError(t, err)
	assert.Equal(t, []string{"a.example.com"}, profile.(*PEM).Hosts)
}

func Test_Construct_Files(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "a.example.com", "*.wild.example.com")

	r := mustBuild(t, map[string]any{
		"name":            "pem",
		"id":              "a",
		"certificatefile": testutil.WriteTempFile(t, "c.pem", leaf.CertPEM),
		"keyfile":         testutil.WriteTempFile(t, "k.pem", leaf.KeyPEM),
		"cafile":          testutil.WriteTempFile(t, "ca.pem", ca.CertPEM),
		"hosts":           []string{"A.example.com", "x.wild.example.com"},
		"minversion":      "1.3",
	})

	profile, err := r.ServerProfile("a")
	require.NoError(t, err)
	assert.Equal(t, []string{"a.example.com", "x.wild.example.com"}, profile.(*PEM).Hosts)
	assert.Equal(t, uint16(tls.VersionTLS13), profile.(*PEM).MinVersion)

	pool, err := r.ServerClientCAs("a")
	require.NoError(t, err)
	assert.NotNil(t, pool)
}

func Test_ACME_Defaults(t *testing.T) {
	r := mustBuild(t, map[string]any{"name": "acme", "id": "a", "hosts": []string{"Tiles.Example.com"}})

	profile, err := r.ServerProfile("a")
	require.NoError(t, err)

	acmeProfile, ok := profile.(*ACME)
	require.True(t, ok)
	assert.Equal(t, "a", acmeProfile.GetID())
	assert.Equal(t, "./certs", acmeProfile.Cache)
	assert.Equal(t, []string{"tiles.example.com"}, acmeProfile.Hosts)
	assert.Equal(t, uint16(tls.VersionTLS12), acmeProfile.MinVersion)
}

func Test_ClientTransport_References(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "a.example.com")
	yes, no := true, false

	r := mustBuild(t,
		map[string]any{"name": "pem", "id": "full", "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "ca": string(ca.CertPEM)},
		map[string]any{"name": "pem", "id": "caonly", "ca": string(ca.CertPEM)},
		map[string]any{"name": "acme", "id": "public", "hosts": []string{"a.example.com"}},
	)

	failures := map[string]config.ClientConfig{
		"unknown tls":         {TLS: "nope"},
		"unknown mtls":        {MTLS: "nope"},
		"unknown disabled":    {MTLS: "nope", EnableMTLS: &no},
		"acme tls":            {TLS: "public"},
		"acme mtls":           {MTLS: "public"},
		"mtls without cert":   {MTLS: "caonly"},
		"mtls system":         {MTLS: "system"},
		"enable without mtls": {EnableMTLS: &yes},
		"enable tls has none": {TLS: "caonly", EnableMTLS: &yes},
	}

	for name, c := range failures {
		t.Run(name, func(t *testing.T) {
			_, err := r.ClientTransport(c)
			require.Error(t, err)
		})
	}

	successes := map[string]config.ClientConfig{
		"tls only":        {TLS: "caonly"},
		"mtls":            {MTLS: "full"},
		"both":            {TLS: "caonly", MTLS: "full"},
		"enable uses tls": {TLS: "full", EnableMTLS: &yes},
		"opted out":       {TLS: "caonly", MTLS: "full", EnableMTLS: &no},
	}

	for name, c := range successes {
		t.Run(name, func(t *testing.T) {
			transport, err := r.ClientTransport(c)
			require.NoError(t, err)
			assert.NotNil(t, transport)
		})
	}

	t.Run("system leaves the default", func(t *testing.T) {
		for _, c := range []config.ClientConfig{{}, {TLS: "system"}, {TLS: "system", EnableMTLS: &no}} {
			transport, err := r.ClientTransport(c)
			require.NoError(t, err)
			assert.Nil(t, transport)
		}
	})

	t.Run("identical references share a pool", func(t *testing.T) {
		first, err := r.ClientTransport(config.ClientConfig{TLS: "caonly"})
		require.NoError(t, err)
		second, err := r.ClientTransport(config.ClientConfig{TLS: "caonly"})
		require.NoError(t, err)
		assert.Same(t, first, second)
	})
}

func Test_ClientTransport_Behavior(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "localhost", "127.0.0.1")

	cert, err := tls.X509KeyPair(leaf.CertPEM, leaf.KeyPEM)
	require.NoError(t, err)

	r := mustBuild(t,
		map[string]any{"name": "pem", "id": "trust", "ca": string(ca.CertPEM), "minversion": "1.3"},
		map[string]any{"name": "pem", "id": "any", "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM)},
		map[string]any{"name": "pem", "id": "scoped", "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "allowedhosts": []string{"other.example.com"}},
		map[string]any{"name": "pem", "id": "here", "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "allowedhosts": []string{"LOCALHOST"}},
	)
	t.Cleanup(func() { require.NoError(t, r.Close(context.Background())) })

	presented := make(chan int, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		presented <- len(req.TLS.PeerCertificates)
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
	upstream.StartTLS()
	defer upstream.Close()

	url := "https://localhost:" + upstream.URL[len("https://127.0.0.1:"):]

	get := func(t *testing.T, c config.ClientConfig) error {
		t.Helper()

		transport, err := r.ClientTransport(c)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		if err == nil {
			resp.Body.Close()
		}

		return err
	}

	t.Run("no CA fails against a private upstream", func(t *testing.T) {
		require.Error(t, get(t, config.ClientConfig{TLS: "any"}))
	})

	cases := map[string]struct {
		mtls  string
		certs int
	}{
		"no mtls":       {"", 0},
		"any host":      {"any", 1},
		"excluded host": {"scoped", 0},
		"included host": {"here", 1},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, get(t, config.ClientConfig{TLS: "trust", MTLS: tc.mtls}))
			assert.Equal(t, tc.certs, <-presented)
		})
	}
}

func Test_HostMatching(t *testing.T) {
	allowed := []string{"*.example.com", "exact.example.net"}

	assert.True(t, allowedHostMatches(allowed, "a.example.com"))
	assert.True(t, allowedHostMatches(allowed, "a.b.example.com"))
	assert.True(t, allowedHostMatches(allowed, "A.EXAMPLE.COM."))
	assert.True(t, allowedHostMatches(allowed, "exact.example.net"))
	assert.False(t, allowedHostMatches(allowed, "example.com"))
	assert.False(t, allowedHostMatches(allowed, "evilexample.com"))
	assert.False(t, allowedHostMatches(allowed, "a.exact.example.net"))

	hosts := []string{"*.example.com", "exact.example.net"}

	assert.True(t, ServerHostMatches(hosts, "a.example.com"))
	assert.True(t, ServerHostMatches(hosts, "EXACT.example.net"))
	assert.False(t, ServerHostMatches(hosts, "a.b.example.com"))
	assert.False(t, ServerHostMatches(hosts, "example.com"))
	assert.False(t, ServerHostMatches(hosts, "evil.com/x.example.com"))
	assert.False(t, ServerHostMatches(nil, "a.example.com"))
}

func Test_ServerReferences(t *testing.T) {
	ca := testutil.NewCA(t, "ca")

	r := mustBuild(t,
		map[string]any{"name": "pem", "id": "caonly", "ca": string(ca.CertPEM)},
		map[string]any{"name": "acme", "id": "public", "hosts": []string{"a.example.com"}},
	)

	for _, id := range []string{"nope", "system", "caonly"} {
		_, err := r.ServerProfile(id)
		require.Error(t, err, id)
	}

	for _, id := range []string{"nope", "system", "public"} {
		_, err := r.ServerClientCAs(id)
		require.Error(t, err, id)
	}

	require.NoError(t, r.CheckServerClientReference("public"))
	require.Error(t, r.CheckServerClientReference("nope"))
}

func Test_NilRegistry(t *testing.T) {
	var r *Registry

	_, err := r.ServerProfile("a")
	require.Error(t, err)

	transport, err := r.ClientTransport(config.ClientConfig{})
	require.NoError(t, err)
	assert.Nil(t, transport)

	r.LogWarnings(context.Background())
	require.NoError(t, r.Close(context.Background()))
}

func Test_LogWarnings(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "a.example.com")

	r := mustBuild(t,
		map[string]any{"name": "pem", "id": "orphan", "ca": string(ca.CertPEM)},
		map[string]any{"name": "pem", "id": "partial", "ca": string(ca.CertPEM), "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "allowedhosts": []string{"a.example.com"}},
		map[string]any{"name": "pem", "id": "clean", "ca": string(ca.CertPEM)},
	)

	_, err := r.ClientTransport(config.ClientConfig{TLS: "partial"})
	require.NoError(t, err)
	_, err = r.ClientTransport(config.ClientConfig{TLS: "clean"})
	require.NoError(t, err)

	r.LogWarnings(context.Background())

	out := buf.String()
	assert.Contains(t, out, "TLS profile orphan is not referenced")
	assert.Contains(t, out, "TLS profile partial sets parameters no reference uses: allowedhosts, certificate, key")
	assert.NotContains(t, out, "clean")
}

func Test_LogWarnings_ProxyWithMTLS(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "a.example.com")

	r := mustBuild(t,
		map[string]any{"name": "pem", "id": "mtls", "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM)},
		map[string]any{"name": "pem", "id": "plain", "ca": string(ca.CertPEM)},
	)

	_, err := r.ClientTransport(config.ClientConfig{TLS: "plain"})
	require.NoError(t, err)

	t.Setenv("HTTPS_PROXY", "https://proxy.example.com")
	r.LogWarnings(context.Background())
	assert.NotContains(t, buf.String(), "HTTPS_PROXY", "no warning without a client certificate in use")

	_, err = r.ClientTransport(config.ClientConfig{MTLS: "mtls"})
	require.NoError(t, err)

	t.Setenv("HTTPS_PROXY", "")
	r.LogWarnings(context.Background())
	assert.NotContains(t, buf.String(), "HTTPS_PROXY", "no warning without a proxy")

	t.Setenv("https_proxy", "https://proxy.example.com")
	r.LogWarnings(context.Background())
	assert.Contains(t, buf.String(), "HTTPS_PROXY is set while TLS profiles mtls present client certificates")
}
