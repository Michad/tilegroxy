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
	"crypto/tls"
	"net/http"
)

func buildTransport(tlsProfile, mtlsProfile *PEM) http.RoundTripper {
	base := &tls.Config{MinVersion: tls.VersionTLS12} // #nosec G402 -- raised by MinVersion in the profile
	if tlsProfile != nil {
		base.MinVersion = tlsProfile.MinVersion
		base.RootCAs = tlsProfile.roots
	}

	plain := cloneDefaultTransport(base)
	if mtlsProfile == nil {
		return plain
	}

	withCert := base.Clone()
	certificate := mtlsProfile.Certificate
	// Certificates would withhold it from servers whose advertised CAs Go can't match to the chain
	withCert.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return certificate, nil
	}

	presenting := cloneDefaultTransport(withCert)
	if len(mtlsProfile.allowedHosts) == 0 {
		return presenting
	}

	return &hostRoutingTransport{allowed: mtlsProfile.allowedHosts, presenting: presenting, plain: plain}
}

// Keeps the environment proxy and pool settings of the default
func cloneDefaultTransport(tlsConfig *tls.Config) *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true}
	}

	transport := base.Clone()
	transport.TLSClientConfig = tlsConfig

	return transport
}

// Separate pools, since a connection that presented the certificate can't be reused for a host that shouldn't see it
type hostRoutingTransport struct {
	allowed    []string
	presenting *http.Transport
	plain      *http.Transport
}

func (t *hostRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if allowedHostMatches(t.allowed, req.URL.Hostname()) {
		return t.presenting.RoundTrip(req)
	}

	return t.plain.RoundTrip(req)
}

func (t *hostRoutingTransport) CloseIdleConnections() {
	t.presenting.CloseIdleConnections()
	t.plain.CloseIdleConnections()
}
