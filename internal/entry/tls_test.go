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
package entry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/internal/testutil"
	"github.com/Michad/tilegroxy/internal/tlsprofiles"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tlsConfig(t *testing.T) config.Config {
	t.Helper()

	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "a.example.com")

	cfg := validConfig()
	cfg.TLS = []map[string]any{
		{"name": "pem", "id": "mesh", "ca": string(ca.CertPEM), "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM)},
		{"name": "pem", "id": "other", "ca": string(ca.CertPEM)},
	}
	cfg.Client.TLS = "mesh"
	cfg.Client.MTLS = "mesh"

	return cfg
}

func Test_ConfigToEntities_ResolvesClientsWithoutEditingLiveConfig(t *testing.T) {
	cfg := tlsConfig(t)

	off := false
	layerClient := &config.ClientConfig{TLS: "other", EnableMTLS: &off}
	cfg.Layers = append(cfg.Layers, config.LayerConfig{ID: "x", Client: layerClient, Provider: map[string]any{"name": "static", "color": "FFFFFF"}})

	ent, err := configToEntities(context.Background(), cfg, nil)
	require.NoError(t, err)
	require.NoError(t, ent.Close(context.Background()))

	// A secret rotation re-resolves this config, so inherited values pinned here would outlive a top-level change
	assert.Same(t, layerClient, cfg.Layers[len(cfg.Layers)-1].Client)
	assert.Empty(t, layerClient.MTLS)
}

func Test_ApplyTLSProfiles_LayersOverridePiecemeal(t *testing.T) {
	cfg := tlsConfig(t)
	off := false
	system := "system"

	cfg.Layers = []config.LayerConfig{
		{ID: "inherit"},
		{ID: "partial", Client: &config.ClientConfig{TLS: "other"}},
		{ID: "optout", Client: &config.ClientConfig{EnableMTLS: &off}},
		{ID: "plain", Client: &config.ClientConfig{TLS: system, EnableMTLS: &off}},
	}

	reg, err := tlsprofiles.Construct(context.Background(), cfg.TLS, nil, cfg.Error.Messages)
	require.NoError(t, err)

	transport, err := applyTLSProfiles(&cfg, reg)
	require.NoError(t, err)
	require.NotNil(t, transport)

	layerTransport := func(i int) http.RoundTripper {
		resolved, resolveErr := reg.ClientTransport(*cfg.Layers[i].Client)
		require.NoError(t, resolveErr)

		return resolved
	}

	assert.Nil(t, cfg.Layers[0].Client)
	assert.Equal(t, "mesh", cfg.Layers[1].Client.MTLS)
	assert.NotEqual(t, transport, layerTransport(1))
	assert.NotEqual(t, transport, layerTransport(2))
	// Top-level profiles must not leak into a layer that asked for plain system validation
	assert.Nil(t, layerTransport(3))
}

func Test_ApplyTLSProfiles_EnableWithoutMTLSKeepsTopLevelCertificate(t *testing.T) {
	cfg := tlsConfig(t)
	on := true
	cfg.Client.MTLS = ""
	cfg.Client.EnableMTLS = &on
	cfg.Layers = []config.LayerConfig{{ID: "a", Client: &config.ClientConfig{TLS: "system"}}, {ID: "b", Client: &config.ClientConfig{TLS: "other"}}}

	reg, err := tlsprofiles.Construct(context.Background(), cfg.TLS, nil, cfg.Error.Messages)
	require.NoError(t, err)

	_, err = applyTLSProfiles(&cfg, reg)
	require.NoError(t, err)
	assert.Equal(t, "mesh", cfg.Layers[0].Client.MTLS)
	assert.Equal(t, "mesh", cfg.Layers[1].Client.MTLS)
}

func Test_BuildDiff_TLSImpact(t *testing.T) {
	profile := func(name, id, hosts string) map[string]any {
		return map[string]any{"name": name, "id": id, "hosts": hosts}
	}

	result := buildDiff(
		map[string]any{"tls": []any{profile("acme", "public", "a"), profile("pem", "private", "a")}},
		map[string]any{"tls": []any{profile("acme", "public", "b"), profile("pem", "private", "b")}},
	)

	assert.Contains(t, result[diffImpactReload][diffKindModified]["tls"], "public")
	assert.Contains(t, result[diffImpactReload][diffKindModified]["tls"], "private")
	assert.NotContains(t, result, diffImpactRestart)
}

func Test_ConfigToEntities_PassthroughOverridesClientProfiles(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	leaf := ca.Issue(t, "127.0.0.1")

	serverCert, err := tls.X509KeyPair(leaf.CertPEM, leaf.KeyPEM)
	require.NoError(t, err)

	clientCAs := x509.NewCertPool()
	require.True(t, clientCAs.AppendCertsFromPEM(ca.CertPEM))

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverCert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	const mesh, other = "mesh", "other"

	tests := []struct {
		name     string
		topTLS   string
		topMTLS  string
		override map[string]any
		pass     bool
	}{
		{name: "top-level only", topTLS: mesh, topMTLS: mesh, pass: true},
		{name: "override grants both", override: map[string]any{"tls": mesh, "mtls": mesh}, pass: true},
		{name: "override keeps inherited certificate", topTLS: other, topMTLS: mesh, override: map[string]any{"timeout": 3}, pass: true},
		{name: "override replaces tls only", topTLS: "system", topMTLS: mesh, override: map[string]any{"tls": other}, pass: true},
		{name: "override replaces mtls only", topTLS: other, override: map[string]any{"mtls": mesh}, pass: true},
		{name: "override opts out of certificate", topTLS: mesh, topMTLS: mesh, override: map[string]any{"enablemtls": false}},
		{name: "override back to system", topTLS: mesh, topMTLS: mesh, override: map[string]any{"tls": "system"}},
		{name: "profile without certificate", override: map[string]any{"tls": other}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.TLS = []map[string]any{
				{"name": "pem", "id": mesh, "ca": string(ca.CertPEM), "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM)},
				{"name": "pem", "id": other, "ca": string(ca.CertPEM)},
			}
			cfg.Client.TLS = tt.topTLS
			cfg.Client.MTLS = tt.topMTLS
			cfg.Authentication = map[string]any{"name": "passthrough", "url": srv.URL, "headers": []string{"X-Api-Key"}}

			if tt.override != nil {
				cfg.Authentication["client"] = tt.override
			}

			ent, err := configToEntities(context.Background(), cfg, nil)
			require.NoError(t, err)

			defer func() { require.NoError(t, ent.Close(context.Background())) }()

			req := httptest.NewRequest(http.MethodGet, "http://tiles/a", nil)
			req.Header.Set("X-Api-Key", "k")

			assert.Equal(t, tt.pass, ent.Auth.CheckAuthentication(pkg.BackgroundContext(), req))
		})
	}
}

func Test_ConfigToEntities_PassthroughUnknownProfileFails(t *testing.T) {
	for _, param := range []string{"tls", "mtls"} {
		cfg := validConfig()
		cfg.Authentication = map[string]any{"name": "passthrough", "url": "https://auth.example.com", "headers": []string{"X-Api-Key"}, "client": map[string]any{param: "missing"}}

		_, err := configToEntities(context.Background(), cfg, nil)
		require.ErrorContains(t, err, "missing")
	}
}
