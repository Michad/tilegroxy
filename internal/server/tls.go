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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal/deprecation"
	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/internal/tlsprofiles"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"golang.org/x/crypto/acme"
)

const (
	defaultHTTPSPort = 443
	legacyACMECache  = "certs"

	// Bounds how long a handshake that never reaches GetCertificate delays retiring its generation
	handshakeHoldLimit = 10 * time.Second
)

func validateEncrypt(enc *config.EncryptionConfig, messages config.ErrorMessages) error {
	if enc == nil {
		return nil
	}

	if enc.TLS == "" && enc.Domain == "" {
		return fmt.Errorf(messages.ParamRequired, "server.encrypt.tls")
	}

	legacy := []struct {
		key         string
		value       string
		replacement string
	}{
		{"server.encrypt.certificate", enc.Certificate, "certificatefile in a pem TLS profile"},
		{"server.encrypt.keyfile", enc.KeyFile, "keyfile in a pem TLS profile"},
		{"server.encrypt.cache", enc.Cache, "cache in an acme TLS profile"},
		{"server.encrypt.domain", enc.Domain, "hosts in a TLS profile"},
	}

	for _, field := range legacy {
		if field.value == "" {
			continue
		}

		if enc.TLS != "" {
			return fmt.Errorf(messages.ParamsMutuallyExclusive, "server.encrypt.tls", field.key)
		}

		deprecation.WarnConfig(field.key, field.replacement, deprecation.NextMajorVersion)
	}

	switch enc.MTLSMode {
	case "", config.MTLSModeRequired, config.MTLSModeOptional, config.MTLSModeDisabled:
		return nil
	}

	return fmt.Errorf(messages.EnumError, "server.encrypt.mtlsmode", enc.MTLSMode, []string{config.MTLSModeRequired, config.MTLSModeOptional, config.MTLSModeDisabled})
}

func effectiveMTLSMode(enc *config.EncryptionConfig) string {
	if enc.MTLSMode != "" {
		return enc.MTLSMode
	}

	if enc.MTLS != "" {
		return config.MTLSModeRequired
	}

	return config.MTLSModeDisabled
}

// What the server references, looked up in one generation of profiles
type encryptReferences struct {
	profile   tlsprofiles.Profile
	clientCAs *x509.CertPool
	mode      string
}

func resolveEncrypt(cfg *config.Config, reg *tlsprofiles.Registry) (encryptReferences, error) {
	enc := cfg.Server.Encrypt
	refs := encryptReferences{mode: effectiveMTLSMode(enc)}

	var err error

	if enc.TLS != "" {
		refs.profile, err = reg.ServerProfile(enc.TLS)
		if err != nil {
			return refs, err
		}

		if _, isACME := refs.profile.(*tlsprofiles.ACME); isACME && enc.HTTPPort == 0 {
			return refs, fmt.Errorf(cfg.Error.Messages.ParamRequired, "server.encrypt.httpport")
		}

		if pem, isPEM := refs.profile.(*tlsprofiles.PEM); isPEM && enc.HTTPPort != 0 && len(pem.Hosts) == 0 {
			slog.WarnContext(pkg.BackgroundContext(), fmt.Sprintf("TLS profile %v has no hosts or DNS names to redirect to, so server.encrypt.httpport will answer every request with a 400", enc.TLS))
		}
	}

	caID := enc.MTLS
	if caID == "" {
		caID = enc.TLS
	}

	if refs.mode == config.MTLSModeDisabled {
		if enc.MTLS != "" {
			err = reg.CheckServerClientReference(enc.MTLS)
		}

		return refs, err
	}

	if caID == "" {
		return refs, fmt.Errorf(cfg.Error.Messages.ParamRequired, "server.encrypt.mtls")
	}

	refs.clientCAs, err = reg.ServerClientCAs(caID)

	return refs, err
}

// Catches what ListenAndServe would reject, without reading deprecated certificate files config check never required
func ValidateTLS(ctx context.Context, cfg *config.Config, reg *tlsprofiles.Registry) error {
	if cfg.Server.Encrypt != nil {
		if _, err := resolveEncrypt(cfg, reg); err != nil {
			return err
		}
	}

	reg.WarnUnused(ctx)

	return nil
}

// One generation's TLS settings, built before the swap so a failure leaves the serving generation untouched
type serverTLSState struct {
	config    *tls.Config
	challenge *tls.Config
	manager   *tlsprofiles.ACMEManager
	redirect  http.Handler
}

func (state *serverTLSState) forClient(hello *tls.ClientHelloInfo) *tls.Config {
	if state.challenge != nil && slices.Equal(hello.SupportedProtos, []string{acme.ALPNProto}) {
		return state.challenge
	}

	return state.config
}

// The listeners and deprecated settings, which outlive generations since the server section isn't reloadable
type serverTLS struct {
	cfg           *config.Config
	gens          *generationHolder
	legacyCert    *tls.Certificate
	legacyManager *tlsprofiles.ACMEManager
	redirect      *http.Server
}

// Nil when the server runs without encryption
func newServerTLS(cfg *config.Config, ent *entities.Entities) (*serverTLS, *serverTLSState, error) {
	if cfg.Server.Encrypt == nil {
		return nil, nil, nil
	}

	s, err := buildServerTLS(cfg)
	if err != nil {
		return nil, nil, err
	}

	state, err := s.buildState(ent.TLS)
	if err != nil {
		s.releaseLegacy()
		return nil, nil, err
	}

	return s, state, nil
}

func buildServerTLS(cfg *config.Config) (*serverTLS, error) {
	s := &serverTLS{cfg: cfg}
	enc := cfg.Server.Encrypt

	switch {
	case enc.TLS != "":
	case enc.Certificate != "" && enc.KeyFile != "":
		cert, err := tls.LoadX509KeyPair(enc.Certificate, enc.KeyFile)
		if err != nil {
			return nil, err
		}

		s.legacyCert = &cert
	default:
		cache := enc.Cache
		if cache == "" {
			cache = legacyACMECache
		}

		s.legacyManager = tlsprofiles.AcquireACME([]string{enc.Domain}, cache)
	}

	return s, nil
}

func (s *serverTLS) attach(gens *generationHolder) {
	if s == nil {
		return
	}

	s.gens = gens

	if httpPort := s.cfg.Server.Encrypt.HTTPPort; httpPort != 0 {
		s.redirect = &http.Server{
			Addr:              net.JoinHostPort(s.cfg.Server.BindHost, strconv.Itoa(httpPort)),
			Handler:           http.HandlerFunc(s.serveRedirect),
			ReadHeaderTimeout: time.Second,
		}
	}
}

func (s *serverTLS) buildState(reg *tlsprofiles.Registry) (*serverTLSState, error) {
	if s == nil {
		return nil, nil
	}

	refs, err := resolveEncrypt(s.cfg, reg)
	if err != nil {
		return nil, err
	}

	state := &serverTLSState{
		config: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"},
			ClientCAs:  refs.clientCAs,
		},
	}

	switch refs.mode {
	case config.MTLSModeRequired:
		state.config.ClientAuth = tls.RequireAndVerifyClientCert
	case config.MTLSModeOptional:
		state.config.ClientAuth = tls.VerifyClientCertIfGiven
	}

	// A resumed session must not outlive a CA removed by a reload
	state.config.SessionTicketsDisabled = state.config.ClientAuth != tls.NoClientCert

	var fallback http.Handler

	enc := s.cfg.Server.Encrypt

	switch profile := refs.profile.(type) {
	case *tlsprofiles.ACME:
		state.config.MinVersion = profile.MinVersion
		state.manager = profile.Manager()
		fallback = hostRedirectHandler{hosts: profile.Hosts, mainPort: s.cfg.Server.Port}
	case *tlsprofiles.PEM:
		state.config.MinVersion = profile.MinVersion
		state.config.Certificates = []tls.Certificate{*profile.Certificate}
		fallback = hostRedirectHandler{hosts: profile.Hosts, mainPort: s.cfg.Server.Port}
	default:
		if s.legacyCert != nil {
			state.config.Certificates = []tls.Certificate{*s.legacyCert}
			fallback = httpRedirectHandler{host: enc.Domain}
		} else {
			state.manager = s.legacyManager
		}
	}

	state.redirect = fallback

	if state.manager != nil {
		state.redirect = state.manager.HTTPHandler(fallback)
		// Let's Encrypt can't present a client certificate, and this config can only negotiate the challenge protocol
		state.challenge = &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{acme.ALPNProto}}
	}

	return state, nil
}

func (s *serverTLS) getConfigForClient(hello *tls.ClientHelloInfo) (*tls.Config, error) {
	gen, release := s.gens.acquire()
	cfg := gen.tls.forClient(hello)

	if gen.tls.manager == nil {
		release()
		return cfg, nil
	}

	// The handshake calls GetCertificate after this returns, so a reload in between mustn't close the manager it uses
	var once sync.Once

	done := func() { once.Do(release) }
	backstop := time.AfterFunc(handshakeHoldLimit, done)
	issue := cfg.GetCertificate

	cfg = cfg.Clone()
	cfg.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		defer done()
		defer backstop.Stop()

		return issue(hello)
	}

	return cfg, nil
}

func (s *serverTLS) serveRedirect(w http.ResponseWriter, req *http.Request) {
	gen, release := s.gens.acquire()
	defer release()

	gen.tls.redirect.ServeHTTP(w, req)
}

func (s *serverTLS) listenAndServe(srvErr chan error, srv *http.Server) {
	if s.redirect != nil {
		go func() { srvErr <- s.redirect.ListenAndServe() }()
	}

	srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: s.getConfigForClient}

	srvErr <- srv.ListenAndServeTLS("", "")
}

func (s *serverTLS) shutdownRedirect(ctx context.Context) error {
	if s == nil || s.redirect == nil {
		return nil
	}

	return s.redirect.Shutdown(ctx)
}

func (s *serverTLS) closeRedirect() error {
	if s == nil || s.redirect == nil {
		return nil
	}

	return s.redirect.Close()
}

func (s *serverTLS) releaseLegacy() {
	if s != nil && s.legacyManager != nil {
		s.legacyManager.Release()
	}
}

// Validates the Host header so a forged one can't redirect a user to another site. Unrecognized hosts go to the first configured one
type hostRedirectHandler struct {
	hosts    []string
	mainPort int
}

func (h hostRedirectHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host, port, err := net.SplitHostPort(req.Host)
	if err != nil {
		host = strings.TrimSuffix(strings.TrimPrefix(req.Host, "["), "]")
		port = ""
	}

	// Brackets are only meaningful around an IPv6 address
	validBrackets := !strings.HasPrefix(req.Host, "[") || net.ParseIP(host) != nil

	if !validBrackets || !tlsprofiles.ServerHostMatches(h.hosts, host) {
		i := slices.IndexFunc(h.hosts, func(entry string) bool { return !strings.HasPrefix(entry, "*") })
		if i < 0 {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		host = h.hosts[i]
	}

	target := host
	if strings.Contains(host, ":") {
		target = "[" + host + "]"
	}

	// No port means the client came through a default port, likely mapped, so the public HTTPS port is the default too
	if port != "" && h.mainPort != defaultHTTPSPort {
		target = net.JoinHostPort(host, strconv.Itoa(h.mainPort))
	}

	location := url.URL{Scheme: "https", Host: target, Path: req.URL.Path, RawQuery: req.URL.RawQuery}
	http.Redirect(w, req, location.String(), http.StatusMovedPermanently)
}
