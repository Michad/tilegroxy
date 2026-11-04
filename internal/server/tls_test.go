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
package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/internal/deprecation"
	"github.com/Michad/tilegroxy/internal/testutil"
	"github.com/Michad/tilegroxy/internal/tlsprofiles"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/acme"
)

type noSecrets struct{}

func (noSecrets) Lookup(context.Context, string) (string, string, error) { return "", "", nil }

func (noSecrets) Check(_ context.Context, keys []string) ([]string, error) {
	return make([]string, len(keys)), nil
}

func encryptConfig(enc config.EncryptionConfig) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Server.Port = 8443
	cfg.Server.Encrypt = &enc

	return &cfg
}

func tlsRegistry(t *testing.T, ca *testutil.CA) *tlsprofiles.Registry {
	t.Helper()

	leaf := ca.Issue(t, "tiles.example.com", "*.wild.example.com")

	reg, err := tlsprofiles.Construct(context.Background(), []map[string]any{
		{"name": "pem", "id": "serving", "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "minversion": "1.3"},
		{"name": "pem", "id": "full", "certificate": string(leaf.CertPEM), "key": string(leaf.KeyPEM), "ca": string(ca.CertPEM)},
		{"name": "pem", "id": "clients", "ca": string(ca.CertPEM)},
		{"name": "acme", "id": "public", "hosts": []string{"tiles.example.com"}, "cache": t.TempDir()},
	}, noSecrets{}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, reg.Close(context.Background())) })

	return reg
}

func tlsState(t *testing.T, cfg *config.Config, reg *tlsprofiles.Registry) *serverTLSState {
	t.Helper()

	s, err := buildServerTLS(cfg)
	require.NoError(t, err)

	state, err := s.buildState(reg)
	require.NoError(t, err)

	return state
}

func Test_ValidateEncrypt(t *testing.T) {
	messages := config.DefaultConfig().Error.Messages

	valid := map[string]config.EncryptionConfig{
		"profile":     {TLS: "a"},
		"legacy":      {Domain: "a.example.com"},
		"legacy full": {Domain: "a.example.com", Certificate: "c", KeyFile: "k", Cache: "x"},
		"mode":        {TLS: "a", MTLSMode: config.MTLSModeOptional},
	}

	for name, enc := range valid {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, validateEncrypt(&enc, messages))
		})
	}

	invalid := map[string]config.EncryptionConfig{
		"nothing":          {},
		"no domain":        {Certificate: "c", KeyFile: "k"},
		"both certificate": {TLS: "a", Certificate: "c"},
		"both keyfile":     {TLS: "a", KeyFile: "c"},
		"both cache":       {TLS: "a", Cache: "c"},
		"both domain":      {TLS: "a", Domain: "c"},
		"bad mode":         {TLS: "a", MTLSMode: "sometimes"},
	}

	for name, enc := range invalid {
		t.Run(name, func(t *testing.T) {
			require.Error(t, validateEncrypt(&enc, messages))
		})
	}

	require.NoError(t, validateEncrypt(nil, messages))
}

func Test_ValidateEncrypt_WarnsOnlyForLegacy(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	deprecation.Reset()

	messages := config.DefaultConfig().Error.Messages

	require.NoError(t, validateEncrypt(&config.EncryptionConfig{TLS: "a", MTLS: "b"}, messages))
	assert.Empty(t, buf.String())

	require.NoError(t, validateEncrypt(&config.EncryptionConfig{Domain: "d", Certificate: "c", KeyFile: "k", Cache: "x"}, messages))

	for _, key := range []string{"domain", "certificate", "keyfile", "cache"} {
		assert.Contains(t, buf.String(), "server.encrypt."+key+" is deprecated")
	}
}

func Test_ResolveEncrypt(t *testing.T) {
	reg := tlsRegistry(t, testutil.NewCA(t, "ca"))

	tests := map[string]struct {
		enc  config.EncryptionConfig
		mode string
		bad  bool
	}{
		"plain":                {enc: config.EncryptionConfig{TLS: "serving"}, mode: config.MTLSModeDisabled},
		"mtls defaults strict": {enc: config.EncryptionConfig{TLS: "serving", MTLS: "clients"}, mode: config.MTLSModeRequired},
		"optional":             {enc: config.EncryptionConfig{TLS: "serving", MTLS: "clients", MTLSMode: config.MTLSModeOptional}, mode: config.MTLSModeOptional},
		"disabled wins":        {enc: config.EncryptionConfig{TLS: "serving", MTLS: "clients", MTLSMode: config.MTLSModeDisabled}, mode: config.MTLSModeDisabled},
		"ca from tls profile":  {enc: config.EncryptionConfig{TLS: "full", MTLSMode: config.MTLSModeRequired}, mode: config.MTLSModeRequired},
		"acme with port":       {enc: config.EncryptionConfig{TLS: "public", HTTPPort: 80}, mode: config.MTLSModeDisabled},
		"legacy":               {enc: config.EncryptionConfig{Domain: "d"}, mode: config.MTLSModeDisabled},
		"legacy with mtls":     {enc: config.EncryptionConfig{Domain: "d", MTLS: "clients"}, mode: config.MTLSModeRequired},

		"unknown tls":          {enc: config.EncryptionConfig{TLS: "nope"}, bad: true},
		"system tls":           {enc: config.EncryptionConfig{TLS: "system"}, bad: true},
		"tls without cert":     {enc: config.EncryptionConfig{TLS: "clients"}, bad: true},
		"acme without port":    {enc: config.EncryptionConfig{TLS: "public"}, bad: true},
		"unknown mtls":         {enc: config.EncryptionConfig{TLS: "serving", MTLS: "nope"}, bad: true},
		"unknown disabled":     {enc: config.EncryptionConfig{TLS: "serving", MTLS: "nope", MTLSMode: config.MTLSModeDisabled}, bad: true},
		"mtls without ca":      {enc: config.EncryptionConfig{TLS: "serving", MTLS: "serving"}, bad: true},
		"required without ca":  {enc: config.EncryptionConfig{TLS: "serving", MTLSMode: config.MTLSModeRequired}, bad: true},
		"legacy required none": {enc: config.EncryptionConfig{Domain: "d", MTLSMode: config.MTLSModeRequired}, bad: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			refs, err := resolveEncrypt(encryptConfig(tc.enc), reg)
			if tc.bad {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.mode, refs.mode)
		})
	}
}

func Test_ResolveEncrypt_WarnsWithoutRedirectHosts(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	ipOnly := ca.Issue(t, "10.0.0.1")
	named := ca.Issue(t, "tiles.example.com")

	reg, err := tlsprofiles.Construct(context.Background(), []map[string]any{
		{"name": "pem", "id": "ip-only", "certificate": string(ipOnly.CertPEM), "key": string(ipOnly.KeyPEM)},
		{"name": "pem", "id": "ip-explicit", "certificate": string(ipOnly.CertPEM), "key": string(ipOnly.KeyPEM), "hosts": []string{"10.0.0.1"}},
		{"name": "pem", "id": "named", "certificate": string(named.CertPEM), "key": string(named.KeyPEM)},
	}, noSecrets{}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, reg.Close(context.Background())) })

	tests := map[string]struct {
		enc  config.EncryptionConfig
		warn bool
	}{
		"no hosts":       {enc: config.EncryptionConfig{TLS: "ip-only", HTTPPort: 80}, warn: true},
		"no redirect":    {enc: config.EncryptionConfig{TLS: "ip-only"}},
		"explicit hosts": {enc: config.EncryptionConfig{TLS: "ip-explicit", HTTPPort: 80}},
		"dns names":      {enc: config.EncryptionConfig{TLS: "named", HTTPPort: 80}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			_, err := resolveEncrypt(encryptConfig(tc.enc), reg)
			require.NoError(t, err)

			if tc.warn {
				assert.Contains(t, buf.String(), "TLS profile "+tc.enc.TLS+" has no hosts")
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func Test_NewServerTLS_SelfSigned(t *testing.T) {
	reg, err := tlsprofiles.Construct(context.Background(), []map[string]any{
		{"name": "selfsigned", "id": "dev", "minversion": "1.3"},
	}, noSecrets{}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, reg.Close(context.Background())) })

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	state := tlsState(t, encryptConfig(config.EncryptionConfig{TLS: "dev", HTTPPort: 80}), reg)

	assert.Contains(t, buf.String(), "TLS profile dev serves a self-signed certificate")
	assert.Equal(t, uint16(tls.VersionTLS13), state.config.MinVersion)
	assert.Len(t, state.config.Certificates, 1)
	assert.Nil(t, state.manager)
	assert.Nil(t, state.challenge)

	rw := httptest.NewRecorder()
	state.redirect.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "http://evil.example.net/a", nil))
	assert.Equal(t, "https://localhost/a", rw.Header().Get("Location"))
}

func Test_NewServerTLS_States(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	reg := tlsRegistry(t, ca)

	t.Run("pem with client certificates", func(t *testing.T) {
		state := tlsState(t, encryptConfig(config.EncryptionConfig{TLS: "serving", MTLS: "clients"}), reg)
		assert.Equal(t, uint16(tls.VersionTLS13), state.config.MinVersion)
		assert.Equal(t, tls.RequireAndVerifyClientCert, state.config.ClientAuth)
		assert.True(t, state.config.SessionTicketsDisabled)
		assert.Len(t, state.config.Certificates, 1)
		assert.Nil(t, state.challenge)
	})

	t.Run("optional", func(t *testing.T) {
		state := tlsState(t, encryptConfig(config.EncryptionConfig{TLS: "serving", MTLS: "clients", MTLSMode: config.MTLSModeOptional}), reg)
		assert.Equal(t, tls.VerifyClientCertIfGiven, state.config.ClientAuth)
	})

	t.Run("acme keeps the challenge reachable without weakening normal connections", func(t *testing.T) {
		state := tlsState(t, encryptConfig(config.EncryptionConfig{TLS: "public", MTLS: "clients", HTTPPort: 80}), reg)

		challenge := state.forClient(&tls.ClientHelloInfo{SupportedProtos: []string{acme.ALPNProto}})
		assert.Equal(t, tls.NoClientCert, challenge.ClientAuth)
		assert.Equal(t, []string{acme.ALPNProto}, challenge.NextProtos)

		// Offering a real protocol alongside must not select the unauthenticated config
		mixed := state.forClient(&tls.ClientHelloInfo{SupportedProtos: []string{acme.ALPNProto, "h2"}})
		assert.Equal(t, tls.RequireAndVerifyClientCert, mixed.ClientAuth)

		normal := state.forClient(&tls.ClientHelloInfo{})
		assert.Equal(t, tls.RequireAndVerifyClientCert, normal.ClientAuth)
	})

	t.Run("legacy keypair failure stops startup", func(t *testing.T) {
		_, err := buildServerTLS(encryptConfig(config.EncryptionConfig{Domain: "d", Certificate: "/nope", KeyFile: "/nope"}))
		require.Error(t, err)
	})

	t.Run("legacy partial keypair falls back to acme", func(t *testing.T) {
		state := tlsState(t, encryptConfig(config.EncryptionConfig{Domain: "d", Certificate: "/nope", Cache: t.TempDir()}), nil)
		assert.NotNil(t, state.manager)
	})
}

func Test_ServerTLS_Reload(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	reg := tlsRegistry(t, ca)

	s, err := buildServerTLS(encryptConfig(config.EncryptionConfig{TLS: "serving", MTLS: "clients", HTTPPort: 80}))
	require.NoError(t, err)

	empty, err := tlsprofiles.Construct(context.Background(), nil, noSecrets{}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	_, err = s.buildState(empty)
	require.Error(t, err)

	pemState, err := s.buildState(reg)
	require.NoError(t, err)
	assert.Nil(t, pemState.manager)

	swapped, err := tlsprofiles.Construct(context.Background(), []map[string]any{
		{"name": "acme", "id": "serving", "hosts": []string{"a.example.com"}, "cache": t.TempDir()},
		{"name": "pem", "id": "clients", "ca": string(ca.CertPEM)},
	}, noSecrets{}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	acmeState, err := s.buildState(swapped)
	require.NoError(t, err)
	assert.NotNil(t, acmeState.manager)
	require.NoError(t, swapped.Close(context.Background()))

	withoutPort, err := buildServerTLS(encryptConfig(config.EncryptionConfig{TLS: "serving"}))
	require.NoError(t, err)

	swappedAgain, err := tlsprofiles.Construct(context.Background(), []map[string]any{
		{"name": "acme", "id": "serving", "hosts": []string{"a.example.com"}, "cache": t.TempDir()},
	}, noSecrets{}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	_, err = withoutPort.buildState(swappedAgain)
	require.Error(t, err)

	var nilTLS *serverTLS

	state, err := nilTLS.buildState(reg)
	require.NoError(t, err)
	assert.Nil(t, state)
	require.NoError(t, nilTLS.shutdownRedirect(context.Background()))
	require.NoError(t, nilTLS.closeRedirect())
	nilTLS.releaseLegacy()
}

func Test_ServerTLS_FollowsCurrentGeneration(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	reg := tlsRegistry(t, ca)
	cfg := encryptConfig(config.EncryptionConfig{TLS: "serving", HTTPPort: 8080})

	s, err := buildServerTLS(cfg)
	require.NoError(t, err)

	first := newGeneration(cfg, nil)
	first.useTLS(tlsState(t, cfg, reg))

	gens := newGenerationHolder(first)
	s.attach(gens)

	hello := &tls.ClientHelloInfo{}

	selected, err := s.getConfigForClient(hello)
	require.NoError(t, err)
	assert.Same(t, first.tls.config, selected)

	second := first.succeededBy(nil)
	second.useTLS(tlsState(t, cfg, reg))
	gens.mu.Lock()
	gens.current = second
	gens.mu.Unlock()

	selected, err = s.getConfigForClient(hello)
	require.NoError(t, err)
	assert.Same(t, second.tls.config, selected)

	req := httptest.NewRequest(http.MethodGet, "/a", nil)
	req.Host = "tiles.example.com"

	rw := httptest.NewRecorder()
	s.redirect.Handler.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusMovedPermanently, rw.Code)
	assert.Zero(t, second.inFlight())
}

func Test_Generation_HoldsDuringACMEHandshake(t *testing.T) {
	ca := testutil.NewCA(t, "ca")
	reg := tlsRegistry(t, ca)
	cfg := encryptConfig(config.EncryptionConfig{TLS: "public", HTTPPort: 80})

	gen := newGeneration(cfg, nil)
	gen.useTLS(tlsState(t, cfg, reg))

	// Rejected by the host policy without any network access
	_, err := gen.tls.config.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.example.net"})
	require.Error(t, err)
	assert.Zero(t, gen.inFlight())

	_, err = gen.tls.challenge.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.example.net"})
	require.Error(t, err)
}

func Test_ServerTLS_RedirectStops(t *testing.T) {
	cfg := encryptConfig(config.EncryptionConfig{TLS: "serving", HTTPPort: 8080})

	s, err := buildServerTLS(cfg)
	require.NoError(t, err)
	s.attach(newGenerationHolder(newGeneration(cfg, nil)))

	require.NoError(t, s.shutdownRedirect(context.Background()))
	require.ErrorIs(t, s.redirect.ListenAndServe(), http.ErrServerClosed)
	require.NoError(t, s.closeRedirect())
}

func Test_ValidateTLS(t *testing.T) {
	reg := tlsRegistry(t, testutil.NewCA(t, "ca"))
	ctx := context.Background()

	require.NoError(t, ValidateTLS(ctx, encryptConfig(config.EncryptionConfig{TLS: "serving"}), reg))
	require.Error(t, ValidateTLS(ctx, encryptConfig(config.EncryptionConfig{TLS: "nope"}), reg))

	// Deprecated certificate files aren't read, so config check accepts them wherever it runs
	require.NoError(t, ValidateTLS(ctx, encryptConfig(config.EncryptionConfig{Domain: "d", Certificate: "/nope", KeyFile: "/nope"}), reg))

	noEncrypt := config.DefaultConfig()
	require.NoError(t, ValidateTLS(ctx, &noEncrypt, reg))
}

func Test_HostRedirectHandler(t *testing.T) {
	reg := tlsRegistry(t, testutil.NewCA(t, "ca"))

	h := tlsState(t, encryptConfig(config.EncryptionConfig{TLS: "serving", HTTPPort: 8080}), reg).redirect

	tests := map[string]struct {
		host     string
		status   int
		location string
	}{
		"default port":     {"tiles.example.com", http.StatusMovedPermanently, "https://tiles.example.com/a/b?c=d"},
		"explicit port":    {"tiles.example.com:8080", http.StatusMovedPermanently, "https://tiles.example.com:8443/a/b?c=d"},
		"wildcard level":   {"x.wild.example.com", http.StatusMovedPermanently, "https://x.wild.example.com/a/b?c=d"},
		"too deep":         {"x.y.wild.example.com", http.StatusMovedPermanently, "https://tiles.example.com/a/b?c=d"},
		"forged":           {"evil.example.net", http.StatusMovedPermanently, "https://tiles.example.com/a/b?c=d"},
		"forged with port": {"evil.example.net:8080", http.StatusMovedPermanently, "https://tiles.example.com:8443/a/b?c=d"},
		"ipv6":             {"[::1]:8080", http.StatusMovedPermanently, "https://tiles.example.com:8443/a/b?c=d"},
		"empty":            {"", http.StatusMovedPermanently, "https://tiles.example.com/a/b?c=d"},
		"bracketed name":   {"[tiles.example.com]", http.StatusMovedPermanently, "https://tiles.example.com/a/b?c=d"},
		"bracketed port":   {"[tiles.example.com]:8080", http.StatusMovedPermanently, "https://tiles.example.com:8443/a/b?c=d"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/a/b?c=d", nil)
			req.Host = tc.host

			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, req)

			assert.Equal(t, tc.status, rw.Code)
			assert.Equal(t, tc.location, rw.Header().Get("Location"))
		})
	}

	t.Run("main port 443 is left implicit", func(t *testing.T) {
		cfg := encryptConfig(config.EncryptionConfig{TLS: "serving", HTTPPort: 8080})
		cfg.Server.Port = 443

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "tiles.example.com:8080"

		rw := httptest.NewRecorder()
		tlsState(t, cfg, reg).redirect.ServeHTTP(rw, req)

		assert.Equal(t, "https://tiles.example.com/", rw.Header().Get("Location"))
	})
}

func Test_RedirectHandler_Legacy(t *testing.T) {
	cert := testutil.NewCA(t, "ca").Issue(t, "tiles.example.com")

	state := tlsState(t, encryptConfig(config.EncryptionConfig{
		Domain:      "tiles.example.com",
		Certificate: testutil.WriteTempFile(t, "c.pem", cert.CertPEM),
		KeyFile:     testutil.WriteTempFile(t, "k.pem", cert.KeyPEM),
		HTTPPort:    8080,
	}), nil)

	req := httptest.NewRequest(http.MethodGet, "/a", nil)
	req.Host = "anything.example.net:8080"

	rw := httptest.NewRecorder()
	state.redirect.ServeHTTP(rw, req)

	// The deprecated format pins the destination to the domain, whatever the Host header says
	assert.Equal(t, "https://tiles.example.com/a", rw.Header().Get("Location"))

	acmeLegacy := tlsState(t, encryptConfig(config.EncryptionConfig{Domain: "tiles.example.com", Cache: t.TempDir(), HTTPPort: 8080}), nil)

	req = httptest.NewRequest(http.MethodGet, "/a", nil)
	req.Host = "tiles.example.com"

	rw = httptest.NewRecorder()
	acmeLegacy.redirect.ServeHTTP(rw, req)

	assert.True(t, strings.HasPrefix(rw.Header().Get("Location"), "https://tiles.example.com"))
}
