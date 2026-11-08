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

// Package tlsprofiles builds the reusable TLS profiles referenced by the client and server configuration
package tlsprofiles

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// The reserved ID for default Go validation against the system CAs
const SystemID = "system"

const (
	pathClientTLS  = "client.tls"
	pathClientMTLS = "client.mtls"
	pathServerTLS  = "server.encrypt.tls"
	pathServerMTLS = "server.encrypt.mtls"
)

type transportKey struct {
	tls  string
	mtls string
}

type idleCloser interface {
	CloseIdleConnections()
}

// One generation of profiles. A reload builds a new one, which is what re-reads the files
type Registry struct {
	profiles   map[string]Profile
	order      []string
	messages   config.ErrorMessages
	transports map[transportKey]http.RoundTripper
}

func Construct(ctx context.Context, raw []map[string]any, secreter secret.Secreter, messages config.ErrorMessages) (*Registry, error) {
	r := &Registry{
		profiles:   make(map[string]Profile, len(raw)),
		messages:   messages,
		transports: map[transportKey]http.RoundTripper{},
	}

	for i, entry := range raw {
		path := fmt.Sprintf("tls[%d]", i)

		resolved, err := configload.ReplaceConfigValues(configload.ReplaceEnv(entry), "secret", func(k string) (string, error) {
			v, _, lookupErr := secreter.Lookup(ctx, k)
			return v, lookupErr
		})
		if err != nil {
			return nil, err
		}

		// Same risk as an empty CA file, for a reference to an unset variable or secret
		for k, v := range entry {
			if original, ok := v.(string); ok && original != "" && resolved[k] == "" {
				return nil, fmt.Errorf(messages.InvalidParam, path+"."+strings.ToLower(k), original)
			}
		}

		profile, err := constructProfile(resolved, Deps{Path: path, ErrorMessages: messages})
		if err != nil {
			return nil, err
		}

		id := profile.GetID()
		if _, exists := r.profiles[id]; exists {
			return nil, fmt.Errorf(messages.MustBeUnique, path+".id", id)
		}

		r.profiles[id] = profile
		r.order = append(r.order, id)
	}

	return r, nil
}

func (r *Registry) lookup(id, path string) (Profile, error) {
	if r != nil {
		if profile, ok := r.profiles[id]; ok {
			return profile, nil
		}
	}

	return nil, fmt.Errorf(r.errorMessages().InvalidParam, path, id)
}

// Never empty so error construction works on a registry-less entity set
func (r *Registry) errorMessages() config.ErrorMessages {
	if r == nil {
		return config.DefaultConfig().Error.Messages
	}

	return r.messages
}

// Returns nil for plain system validation so callers keep using http.DefaultTransport
func (r *Registry) ClientTransport(c config.ClientConfig) (http.RoundTripper, error) {
	key, tlsProfile, mtlsProfile, err := r.resolveClient(c)
	if err != nil {
		return nil, err
	}

	if tlsProfile == nil && mtlsProfile == nil {
		return nil, nil
	}

	if transport, ok := r.transports[key]; ok {
		return transport, nil
	}

	transport := buildTransport(tlsProfile, mtlsProfile)
	r.transports[key] = transport

	return transport, nil
}

func (r *Registry) resolveClient(c config.ClientConfig) (transportKey, *PEM, *PEM, error) {
	key := transportKey{tls: c.TLS}
	if key.tls == "" {
		key.tls = SystemID
	}

	var tlsProfile *PEM

	if key.tls != SystemID {
		profile, err := r.lookup(key.tls, pathClientTLS)
		if err != nil {
			return key, nil, nil, err
		}

		var isPEM bool

		tlsProfile, isPEM = profile.(*PEM)
		if !isPEM {
			return key, nil, nil, fmt.Errorf(r.messages.InvalidParam, pathClientTLS, key.tls)
		}

		tlsProfile.markUsed(paramCA, paramCAFile, paramSystemCAs, paramMinVersion)
	}

	enabled := c.MTLS != ""
	if c.EnableMTLS != nil {
		enabled = *c.EnableMTLS
	}

	mtlsID := c.MTLS
	if mtlsID == "" {
		mtlsID = key.tls
	}

	if !enabled {
		if c.MTLS != "" && c.MTLS != SystemID {
			if _, err := r.lookup(c.MTLS, pathClientMTLS); err != nil {
				return key, nil, nil, err
			}
		}

		return key, tlsProfile, nil, nil
	}

	profile, err := r.lookup(mtlsID, pathClientMTLS)
	if err != nil {
		return key, nil, nil, err
	}

	mtlsProfile, isPEM := profile.(*PEM)
	if !isPEM || mtlsProfile.Certificate == nil {
		return key, nil, nil, fmt.Errorf(r.errorMessages().InvalidParam, pathClientMTLS, mtlsID)
	}

	mtlsProfile.markUsed(paramCertificate, paramCertificateFile, paramKey, paramKeyFile, paramAllowedHosts)
	key.mtls = mtlsID

	return key, tlsProfile, mtlsProfile, nil
}

// The profile supplying the certificate served to clients
func (r *Registry) ServerProfile(id string) (Profile, error) {
	profile, err := r.lookup(id, pathServerTLS)
	if err != nil {
		return nil, err
	}

	if pem, isPEM := profile.(*PEM); isPEM && pem.Certificate == nil {
		return nil, fmt.Errorf(r.errorMessages().InvalidParam, pathServerTLS, id)
	}

	profile.base().markUsed(paramCertificate, paramCertificateFile, paramKey, paramKeyFile, paramMinVersion, paramHosts, paramCache)

	return profile, nil
}

// The CAs validating incoming client certificates. System CAs never apply here
func (r *Registry) ServerClientCAs(id string) (*x509.CertPool, error) {
	profile, err := r.lookup(id, pathServerMTLS)
	if err != nil {
		return nil, err
	}

	pem, isPEM := profile.(*PEM)
	if !isPEM || pem.clientCAs == nil {
		return nil, fmt.Errorf(r.errorMessages().InvalidParam, pathServerMTLS, id)
	}

	pem.markUsed(paramCA, paramCAFile)

	return pem.clientCAs, nil
}

// For a reference that's validated even when its mode leaves it unused
func (r *Registry) CheckServerClientReference(id string) error {
	_, err := r.lookup(id, pathServerMTLS)
	return err
}

func (r *Registry) LogWarnings(ctx context.Context) {
	if r == nil {
		return
	}

	r.warnProxyWithMTLS(ctx)

	for _, id := range r.order {
		profile := r.profiles[id].base()

		if !profile.referenced {
			slog.WarnContext(ctx, fmt.Sprintf("TLS profile %v is not referenced by client or server.encrypt", id))
			continue
		}

		var unused []string

		for param, set := range profile.set {
			if set && !profile.used[param] {
				unused = append(unused, param)
			}
		}

		if len(unused) > 0 {
			slices.Sort(unused)
			slog.WarnContext(ctx, fmt.Sprintf("TLS profile %v sets parameters no reference uses: %v", id, strings.Join(unused, ", ")))
		}
	}
}

// Go handshakes with an https:// proxy using the target's TLS config, client certificate included
func (r *Registry) warnProxyWithMTLS(ctx context.Context) {
	if os.Getenv("HTTPS_PROXY") == "" && os.Getenv("https_proxy") == "" {
		return
	}

	var ids []string

	for key := range r.transports {
		if key.mtls != "" && !slices.Contains(ids, key.mtls) {
			ids = append(ids, key.mtls)
		}
	}

	if len(ids) == 0 {
		return
	}

	slices.Sort(ids)
	slog.WarnContext(ctx, fmt.Sprintf("HTTPS_PROXY is set while TLS profiles %v present client certificates. An https:// proxy is sent the certificate regardless of allowedHosts and is validated against the profile's CAs", strings.Join(ids, ", ")))
}

func (r *Registry) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}

	errs := make([]error, 0, len(r.profiles))

	for _, profile := range r.profiles {
		errs = append(errs, lifecycle.CloseIfCloser(ctx, profile))
	}

	for _, transport := range r.transports {
		if closer, ok := transport.(idleCloser); ok {
			closer.CloseIdleConnections()
		}
	}

	return errors.Join(errs...)
}
