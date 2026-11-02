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
	"crypto/x509"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serverSelfSigned(t *testing.T, r *Registry, id string) *SelfSigned {
	t.Helper()

	profile, err := r.ServerProfile(id)
	require.NoError(t, err)

	selfSigned, ok := profile.(*SelfSigned)
	require.True(t, ok)

	return selfSigned
}

func Test_SelfSigned_DefaultsToLoopback(t *testing.T) {
	r := mustBuild(t, map[string]any{"name": "selfsigned", "id": "dev"})
	p := serverSelfSigned(t, r, "dev")

	assert.Equal(t, []string{"localhost", "127.0.0.1", "::1"}, p.Hosts)
	assert.Equal(t, uint16(tls.VersionTLS12), p.MinVersion)

	roots := x509.NewCertPool()
	roots.AddCert(p.Certificate.Leaf)

	for _, host := range p.Hosts {
		_, err := p.Certificate.Leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: roots})
		require.NoError(t, err, host)
	}
}

func Test_SelfSigned_CustomParameters(t *testing.T) {
	r := mustBuild(t, map[string]any{
		"name": "selfsigned", "id": "dev", "minversion": "1.3", "hosts": []string{"Tiles.Test.", "10.0.0.1"},
	})
	p := serverSelfSigned(t, r, "dev")

	assert.Equal(t, []string{"tiles.test", "10.0.0.1"}, p.Hosts)
	assert.Equal(t, uint16(tls.VersionTLS13), p.MinVersion)
	require.NoError(t, p.Certificate.Leaf.VerifyHostname("tiles.test"))
	require.NoError(t, p.Certificate.Leaf.VerifyHostname("10.0.0.1"))
	require.Error(t, p.Certificate.Leaf.VerifyHostname("localhost"))
}

func Test_SelfSigned_GeneratesPerConstruction(t *testing.T) {
	entry := map[string]any{"name": "selfsigned", "id": "dev"}

	first := serverSelfSigned(t, mustBuild(t, entry), "dev")
	second := serverSelfSigned(t, mustBuild(t, entry), "dev")

	assert.NotEqual(t, first.Certificate.Certificate, second.Certificate.Certificate)
}

func Test_SelfSigned_ServerCertificateOnly(t *testing.T) {
	r := mustBuild(t, map[string]any{"name": "selfsigned", "id": "dev"})

	_, err := r.ClientTransport(config.ClientConfig{TLS: "dev"})
	require.Error(t, err)

	_, err = r.ClientTransport(config.ClientConfig{MTLS: "dev"})
	require.Error(t, err)

	_, err = r.ServerClientCAs("dev")
	require.Error(t, err)
}
