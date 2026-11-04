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
//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tlsTilePath = "/tiles/color/8/12/32"
	// Must be valid YAML indentation beneath a top-level layers key
	tlsColorLayer = `
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`
)

// One CA with PEM files on disk for a server certificate and a client certificate
type tlsFiles struct {
	ca         *testutil.CA
	caFile     string
	certFile   string
	keyFile    string
	clientCert tls.Certificate
	httpPort   int
}

func newTLSFiles(t *testing.T) tlsFiles {
	t.Helper()

	ca := testutil.NewCA(t, "e2e")
	server := ca.Issue(t, "localhost", "tiles.test")
	client := ca.Issue(t, "client")

	clientCert, err := tls.X509KeyPair(client.CertPEM, client.KeyPEM)
	require.NoError(t, err)

	return tlsFiles{
		ca:         ca,
		caFile:     testutil.WriteTempFile(t, "ca.pem", ca.CertPEM),
		certFile:   testutil.WriteTempFile(t, "cert.pem", server.CertPEM),
		keyFile:    testutil.WriteTempFile(t, "key.pem", server.KeyPEM),
		clientCert: clientCert,
		httpPort:   testutil.FreePort(t),
	}
}

func httpsClient(t *testing.T, caPEM []byte, certs ...tls.Certificate) *http.Client {
	t.Helper()

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))

	tlsConfig := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}

	// Certificates would silently withhold one the server's advertised CAs don't match, hiding whether the server rejects it
	if len(certs) > 0 {
		tlsConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &certs[0], nil
		}
	}

	return &http.Client{
		Timeout:   Scale(requestTimeout),
		Transport: &http.Transport{TLSClientConfig: tlsConfig, DisableKeepAlives: true},
	}
}

func (i *Instance) httpsURL(path string) string {
	return "https://localhost:" + strconv.Itoa(i.ports.Server) + path
}

func getStatus(client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, err
}

func expectServed(t *testing.T, inst *Instance, client *http.Client) {
	t.Helper()

	status, err := getStatus(client, inst.httpsURL(tlsTilePath))
	require.NoErrorf(t, err, "Output:\n%s", inst.Output())
	assert.Equal(t, http.StatusOK, status)
}

// A rejected client certificate can surface on either side of the handshake depending on TLS version
func expectRejected(t *testing.T, inst *Instance, client *http.Client) {
	t.Helper()

	_, err := getStatus(client, inst.httpsURL(tlsTilePath))
	require.Error(t, err)
}

func waitForPort(t *testing.T, port int) {
	t.Helper()

	Until(t, Scale(startupTimeout), "redirect port to bind", func() bool {
		dialer := net.Dialer{Timeout: probeTimeout}

		conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return false
		}

		return conn.Close() == nil
	})
}

// Sent with a chosen Host header since nothing resolves the test hostnames
func redirectFrom(t *testing.T, port int, host string) (int, string) {
	t.Helper()

	waitForPort(t, port)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/tiles/a?b=c", nil)
	require.NoError(t, err)

	req.Host = host

	client := &http.Client{
		Timeout:       Scale(requestTimeout),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	return resp.StatusCode, resp.Header.Get("Location")
}

func serverYAML(encrypt string) string {
	return fmt.Sprintf(`
server:
  port: {{.Port}}
  drainDelay: 0
  encrypt:
%s
`, encrypt)
}

func checkConfig(t *testing.T, raw string) (string, int) {
	t.Helper()

	return Run(t, "config", "check", "-c", writeConfig(t, raw))
}

func Test_TLS_Deprecated_CertificateAndKeyfile(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: serverYAML(fmt.Sprintf(`
    domain: tiles.test
    certificate: %v
    keyfile: %v
    httpport: %v`, f.certFile, f.keyFile, f.httpPort)) + tlsColorLayer})

	expectServed(t, inst, httpsClient(t, f.ca.CertPEM))

	for _, key := range []string{"domain", "certificate", "keyfile"} {
		assert.Contains(t, inst.Output(), "server.encrypt."+key+" is deprecated")
	}

	// The deprecated format always pins to its domain, whatever the Host header says
	status, location := redirectFrom(t, f.httpPort, "anything.example.net")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://tiles.test/tiles/a?b=c", location)
}

func Test_TLS_Deprecated_CapitalizedKeys(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: fmt.Sprintf(`
Server:
  Port: {{.Port}}
  DrainDelay: 0
  Encrypt:
    Domain: tiles.test
    Certificate: %v
    KeyFile: %v
`, f.certFile, f.keyFile) + tlsColorLayer})

	expectServed(t, inst, httpsClient(t, f.ca.CertPEM))
}

func Test_TLS_Deprecated_LetsEncrypt(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: serverYAML(fmt.Sprintf(`
    domain: tiles.test
    cache: %v
    httpport: %v`, t.TempDir(), f.httpPort)) + tlsColorLayer})

	status, location := redirectFrom(t, f.httpPort, "tiles.test")
	assert.Equal(t, http.StatusFound, status)
	assert.Equal(t, "https://tiles.test/tiles/a?b=c", location)

	for _, key := range []string{"domain", "cache"} {
		assert.Contains(t, inst.Output(), "server.encrypt."+key+" is deprecated")
	}
}

func Test_TLS_Deprecated_WithClientCertificates(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: fmt.Sprintf(`
tls:
  - name: pem
    id: clients
    caFile: %v
`, f.caFile) + serverYAML(fmt.Sprintf(`
    domain: tiles.test
    certificate: %v
    keyfile: %v
    mtls: clients`, f.certFile, f.keyFile)) + tlsColorLayer})

	expectRejected(t, inst, httpsClient(t, f.ca.CertPEM))
	expectServed(t, inst, httpsClient(t, f.ca.CertPEM, f.clientCert))
}

func Test_TLS_Deprecated_MixedWithProfileIsRejected(t *testing.T) {
	out, code := checkConfig(t, `
server:
  encrypt:
    tls: a
    domain: tiles.test
`+tlsColorLayer)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "server.encrypt.domain")
}

func Test_TLS_Deprecated_CheckAcceptsWithoutReadingFiles(t *testing.T) {
	out, code := checkConfig(t, `
server:
  encrypt:
    domain: tiles.test
    certificate: /nonexistent/cert.pem
    keyfile: /nonexistent/key.pem
`+tlsColorLayer)

	assert.Equal(t, 0, code, "output: %s", out)
	assert.Contains(t, out, "server.encrypt.certificate is deprecated")
}

func pemProfile(f tlsFiles) string {
	return fmt.Sprintf(`
tls:
  - name: pem
    id: main
    certificateFile: %v
    keyFile: %v
    minVersion: "1.2"
    hosts:
      - tiles.test
  - name: pem
    id: clients
    caFile: %v
`, f.certFile, f.keyFile, f.caFile)
}

func Test_TLS_Profile_PEM(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: pemProfile(f) + serverYAML(fmt.Sprintf(`
    tls: main
    httpPort: %v`, f.httpPort)) + tlsColorLayer})

	expectServed(t, inst, httpsClient(t, f.ca.CertPEM))
	assert.NotContains(t, inst.Output(), "deprecated")
	assert.Contains(t, inst.Output(), "TLS profile clients is not referenced")

	mainPort := strconv.Itoa(inst.ports.Server)

	status, location := redirectFrom(t, f.httpPort, "tiles.test:"+strconv.Itoa(f.httpPort))
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://tiles.test:"+mainPort+"/tiles/a?b=c", location)

	status, location = redirectFrom(t, f.httpPort, "tiles.test")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://tiles.test/tiles/a?b=c", location)

	// Covered by the certificate but not in hosts
	status, location = redirectFrom(t, f.httpPort, "localhost")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://tiles.test/tiles/a?b=c", location)

	status, location = redirectFrom(t, f.httpPort, "evil.example.net")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://tiles.test/tiles/a?b=c", location)
}

func Test_TLS_Profile_FromEnvironment(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{
		Raw: pemProfile(f) + `
server:
  port: {{.Port}}
  drainDelay: 0
` + tlsColorLayer,
		Env: []string{"SERVER_ENCRYPT_TLS=main", "SERVER_ENCRYPT_MTLS=clients"},
	})

	expectRejected(t, inst, httpsClient(t, f.ca.CertPEM))
	expectServed(t, inst, httpsClient(t, f.ca.CertPEM, f.clientCert))
}

func Test_TLS_Profile_PEMText(t *testing.T) {
	f := newTLSFiles(t)
	cert, err := os.ReadFile(f.certFile)
	require.NoError(t, err)
	key, err := os.ReadFile(f.keyFile)
	require.NoError(t, err)

	inst := Start(t, Config{
		Raw: `
tls:
  - name: pem
    id: main
    certificate: env.TEST_TLS_CERT
    key: env.TEST_TLS_KEY
` + serverYAML("    tls: main") + tlsColorLayer,
		Env: []string{"TEST_TLS_CERT=" + string(cert), "TEST_TLS_KEY=" + string(key)},
	})

	expectServed(t, inst, httpsClient(t, f.ca.CertPEM))
}

func Test_TLS_Profile_ACME(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: fmt.Sprintf(`
tls:
  - name: acme
    id: public
    hosts:
      - tiles.test
    cache: %v
`, t.TempDir()) + serverYAML(fmt.Sprintf(`
    tls: public
    httpPort: %v`, f.httpPort)) + tlsColorLayer})

	status, location := redirectFrom(t, f.httpPort, "tiles.test")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://tiles.test/tiles/a?b=c", location)

	status, location = redirectFrom(t, f.httpPort, "evil.example.net")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://tiles.test/tiles/a?b=c", location)

	assert.NotContains(t, inst.Output(), "deprecated")
}

func Test_TLS_Profile_SelfSigned(t *testing.T) {
	inst := Start(t, Config{Raw: `
tls:
  - name: selfsigned
    id: dev
` + serverYAML("    tls: dev") + tlsColorLayer})

	conn, err := tls.Dial("tcp", strings.TrimPrefix(inst.httpsURL(""), "https://"), &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) // #nosec G402 -- fetching the certificate to trust below
	require.NoError(t, err)

	served := conn.ConnectionState().PeerCertificates[0]
	require.NoError(t, conn.Close())

	expectServed(t, inst, httpsClient(t, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: served.Raw})))
	assert.Contains(t, inst.Output(), "self-signed")
}

func Test_TLS_Profile_ClientCertificateModes(t *testing.T) {
	f := newTLSFiles(t)
	strangerCA := testutil.NewCA(t, "stranger")
	strangerLeaf := strangerCA.Issue(t, "stranger")

	stranger, err := tls.X509KeyPair(strangerLeaf.CertPEM, strangerLeaf.KeyPEM)
	require.NoError(t, err)

	tests := map[string]struct {
		encrypt     string
		anonymous   bool
		withCert    bool
		withUnknown bool
	}{
		"required by default": {encrypt: "    tls: main\n    mtls: clients", withCert: true},
		"required":            {encrypt: "    tls: main\n    mtls: clients\n    mtlsMode: required", withCert: true},
		"optional":            {encrypt: "    tls: main\n    mtls: clients\n    mtlsMode: optional", anonymous: true, withCert: true},
		"disabled":            {encrypt: "    tls: main\n    mtls: clients\n    mtlsMode: disabled", anonymous: true, withCert: true, withUnknown: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			inst := Start(t, Config{Raw: pemProfile(f) + serverYAML(tc.encrypt) + tlsColorLayer})

			check := func(allowed bool, client *http.Client) {
				t.Helper()

				if allowed {
					expectServed(t, inst, client)
				} else {
					expectRejected(t, inst, client)
				}
			}

			check(tc.anonymous, httpsClient(t, f.ca.CertPEM))
			check(tc.withCert, httpsClient(t, f.ca.CertPEM, f.clientCert))
			check(tc.withUnknown, httpsClient(t, f.ca.CertPEM, stranger))
		})
	}
}

func Test_TLS_Profile_HealthPortNotAffectedByClientCertificates(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: pemProfile(f) + serverYAML("    tls: main\n    mtls: clients") + `
health:
  enabled: true
  port: {{.HealthPort}}
` + tlsColorLayer})

	Until(t, Scale(startupTimeout), "health to answer", func() bool {
		status, err := getStatus(&http.Client{Timeout: Scale(requestTimeout)}, inst.HealthURL()+"/health")
		return err == nil && status == http.StatusOK
	})
}

func Test_TLS_Deprecated_StaleEnvironmentStaysPlainHTTP(t *testing.T) {
	inst := Start(t, Config{
		Raw: `
server:
  port: {{.Port}}
  drainDelay: 0
` + tlsColorLayer,
		Env: []string{"SERVER_ENCRYPT_DOMAIN=tiles.test", "SERVER_ENCRYPT_HTTPPORT=1"},
	})

	inst.Get(tlsTilePath).ExpectStatus(http.StatusOK)
}

func Test_TLS_Profile_ModeFallsBackToCAInTLSProfile(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: fmt.Sprintf(`
tls:
  - name: pem
    id: main
    certificateFile: %v
    keyFile: %v
    caFile: %v
`, f.certFile, f.keyFile, f.caFile) + serverYAML("    tls: main\n    mtlsMode: required") + tlsColorLayer})

	expectRejected(t, inst, httpsClient(t, f.ca.CertPEM))
	expectServed(t, inst, httpsClient(t, f.ca.CertPEM, f.clientCert))
}

func Test_TLS_Profile_MinVersionEnforced(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: fmt.Sprintf(`
tls:
  - name: pem
    id: main
    certificateFile: %v
    keyFile: %v
    minVersion: "1.3"
`, f.certFile, f.keyFile) + serverYAML("    tls: main") + tlsColorLayer})

	modern := httpsClient(t, f.ca.CertPEM)
	expectServed(t, inst, modern)

	legacy := httpsClient(t, f.ca.CertPEM)
	legacy.Transport.(*http.Transport).TLSClientConfig.MaxVersion = tls.VersionTLS12
	expectRejected(t, inst, legacy)
}

func Test_TLS_Profile_RedirectPortAsksForNoCertificate(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: pemProfile(f) + serverYAML(fmt.Sprintf("    tls: main\n    mtls: clients\n    httpPort: %v", f.httpPort)) + tlsColorLayer})

	status, _ := redirectFrom(t, f.httpPort, "tiles.test")
	assert.Equal(t, http.StatusMovedPermanently, status)
	expectRejected(t, inst, httpsClient(t, f.ca.CertPEM))
}

func Test_TLS_Profile_InvalidReferencesPreventStartup(t *testing.T) {
	f := newTLSFiles(t)

	tests := map[string]string{
		"unknown server profile": "server:\n  encrypt:\n    tls: nope\n",
		"system as server":       "server:\n  encrypt:\n    tls: system\n",
		"unknown client profile": "client:\n  tls: nope\n",
		"acme as client":         "client:\n  tls: public\ntls:\n  - name: acme\n    id: public\n    hosts: [a.example.com]\n",
		"acme without http port": "server:\n  encrypt:\n    tls: public\ntls:\n  - name: acme\n    id: public\n    hosts: [a.example.com]\n",
		"selfsigned as client":   "client:\n  tls: dev\ntls:\n  - name: selfsigned\n    id: dev\n",
		"mtls without a cert":    fmt.Sprintf("client:\n  mtls: ca\ntls:\n  - name: pem\n    id: ca\n    caFile: %v\n", f.caFile),
		"server mtls without ca": pemProfile(f) + "server:\n  encrypt:\n    tls: main\n    mtls: main\n",
		"reserved id":            "tls:\n  - name: pem\n    id: system\n",
		"unquoted min version":   "tls:\n  - name: pem\n    id: a\n    minVersion: 1.3\n",
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			out, code := checkConfig(t, raw+tlsColorLayer)
			assert.NotEqual(t, 0, code, "output: %s", out)
		})
	}

	t.Run("serve exits", func(t *testing.T) {
		path := writeConfig(t, "server:\n  encrypt:\n    tls: nope\n"+tlsColorLayer)

		out, code := Run(t, "serve", "-c", path)
		assert.NotEqual(t, 0, code)
		assert.Contains(t, out, "server.encrypt.tls")
	})
}

// A private upstream that accepts anyone but only serves tiles to clients presenting a certificate
func startUpstream(t *testing.T, f tlsFiles) string {
	t.Helper()

	leaf := f.ca.Issue(t, "localhost")

	cert, err := tls.X509KeyPair(leaf.CertPEM, leaf.KeyPEM)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(f.ca.CertPEM))

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if len(req.TLS.PeerCertificates) == 0 && strings.HasPrefix(req.URL.Path, "/mtls") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "3")
		_, _ = w.Write([]byte("png"))
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pool, MinVersion: tls.VersionTLS12}
	upstream.StartTLS()
	t.Cleanup(upstream.Close)

	_, port, err := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "https://"))
	require.NoError(t, err)

	return "https://localhost:" + port
}

func Test_TLS_Client_Profiles(t *testing.T) {
	f := newTLSFiles(t)
	upstream := startUpstream(t, f)

	layer := func(id, client, path string) string {
		return fmt.Sprintf(`
  - id: %v%v
    provider:
      name: proxy
      url: %v/%v/{z}/{x}/{y}.png
`, id, client, upstream, path)
	}

	inst := Start(t, Config{Raw: fmt.Sprintf(`
server:
  port: {{.Port}}
  drainDelay: 0
error:
  mode: text
tls:
  - name: pem
    id: full
    certificateFile: %[1]v
    keyFile: %[2]v
    caFile: %[3]v
  - name: pem
    id: trust
    caFile: %[3]v
  - name: pem
    id: elsewhere
    certificateFile: %[1]v
    keyFile: %[2]v
    allowedHosts:
      - "*.example.com"
  - name: pem
    id: here
    certificateFile: %[1]v
    keyFile: %[2]v
    allowedHosts:
      - LOCALHOST
client:
  tls: full
  mtls: full
layers:
`, f.certFile, f.keyFile, f.caFile) +
		layer("inherited", "", "mtls") +
		layer("optout", "\n    client:\n      enableMTLS: false", "mtls") +
		layer("optoutplain", "\n    client:\n      enableMTLS: false", "plain") +
		layer("caonly", "\n    client:\n      tls: trust\n      mtls: here", "mtls") +
		layer("excluded", "\n    client:\n      mtls: elsewhere", "mtls") +
		layer("systemca", "\n    client:\n      tls: system\n      enableMTLS: false", "plain") +
		layer("impliedcert", "\n    client:\n      tls: full\n      mtls: \"\"\n      enableMTLS: true", "mtls")})

	expected := map[string]int{
		"inherited":   http.StatusOK,
		"optoutplain": http.StatusOK,
		"caonly":      http.StatusOK,
		"impliedcert": http.StatusOK,
	}

	for _, id := range []string{"inherited", "optout", "optoutplain", "caonly", "excluded", "systemca", "impliedcert"} {
		t.Run(id, func(t *testing.T) {
			resp := inst.Get("/tiles/" + id + "/8/12/32")

			if want, ok := expected[id]; ok {
				resp.ExpectStatus(want)
			} else {
				assert.NotEqual(t, http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
			}
		})
	}
}

func Test_TLS_Client_NoProfilesKeepsDefaultValidation(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: fmt.Sprintf(`
server:
  port: {{.Port}}
  drainDelay: 0
layers:
  - id: private
    provider:
      name: proxy
      url: %v/plain/{z}/{x}/{y}.png
`, startUpstream(t, f))})

	assert.NotEqual(t, http.StatusOK, inst.Get("/tiles/private/8/12/32").StatusCode)
}

func Test_TLS_Reload_RotatesCertificateAndCA(t *testing.T) {
	f := newTLSFiles(t)
	clients := filepath.Join(t.TempDir(), "clients.pem")
	require.NoError(t, os.WriteFile(clients, f.ca.CertPEM, configMode))

	inst := Start(t, Config{Raw: fmt.Sprintf(`
tls:
  - name: pem
    id: main
    certificateFile: %v
    keyFile: %v
  - name: pem
    id: clients
    caFile: %v
`, f.certFile, f.keyFile, clients) + serverYAML("    tls: main\n    mtls: clients") + tlsColorLayer})

	withCert := httpsClient(t, f.ca.CertPEM, f.clientCert)
	expectServed(t, inst, withCert)

	// Rotate to a certificate only the new CA vouches for, and stop trusting the old client CA
	rotated := testutil.NewCA(t, "rotated")
	leaf := rotated.Issue(t, "localhost")
	rotatedClientLeaf := rotated.Issue(t, "client")

	rotatedClient, err := tls.X509KeyPair(rotatedClientLeaf.CertPEM, rotatedClientLeaf.KeyPEM)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(f.certFile, leaf.CertPEM, configMode))
	require.NoError(t, os.WriteFile(f.keyFile, leaf.KeyPEM, configMode))
	require.NoError(t, os.WriteFile(clients, rotated.CertPEM, configMode))

	inst.Signal(syscall.SIGHUP)

	newClient := httpsClient(t, rotated.CertPEM, rotatedClient)

	Until(t, Scale(30*time.Second), "new certificate to be served", func() bool {
		status, err := getStatus(newClient, inst.httpsURL(tlsTilePath))
		return err == nil && status == http.StatusOK
	})

	// Trusts the new server certificate but presents a client certificate from the removed CA
	expectRejected(t, inst, httpsClient(t, rotated.CertPEM, f.clientCert))
}

func Test_TLS_Reload_BrokenFileKeepsServing(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: pemProfile(f) + serverYAML("    tls: main") + tlsColorLayer})

	client := httpsClient(t, f.ca.CertPEM)
	expectServed(t, inst, client)

	require.NoError(t, os.WriteFile(f.certFile, []byte("not a certificate"), configMode))

	inst.Signal(syscall.SIGHUP)

	Until(t, Scale(30*time.Second), "reload failure to be reported", func() bool {
		return strings.Contains(inst.Output(), "tls[0].certificate")
	})

	expectServed(t, inst, client)
}

// A certificate in the directory autocert reads, so nothing contacts Let's Encrypt
func seedACMECache(t *testing.T, ca *testutil.CA, host string) string {
	t.Helper()

	leaf := ca.Issue(t, host)
	cache := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cache, host), slices.Concat(leaf.KeyPEM, leaf.CertPEM), configMode))

	return cache
}

func acmeConfig(host, cache string, httpPort int) string {
	return fmt.Sprintf(`
tls:
  - name: acme
    id: public
    hosts:
      - %v
    cache: %v
`, host, cache) + serverYAML(fmt.Sprintf(`
    tls: public
    httpPort: %v`, httpPort)) + tlsColorLayer
}

// Whether a handshake for host succeeds, dialing localhost since nothing resolves the test hostnames
func acmeServes(inst *Instance, caPEM []byte, host string) bool {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: probeTimeout},
		Config:    &tls.Config{RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS12},
	}

	conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(inst.ports.Server)))
	if err != nil {
		return false
	}

	return conn.Close() == nil
}

func reloadConfig(t *testing.T, inst *Instance, raw string) {
	t.Helper()

	require.NoError(t, os.WriteFile(inst.ConfigPath, []byte(renderConfig(t, raw, inst.ports)), configMode))
	inst.Signal(syscall.SIGHUP)
}

func Test_TLS_Reload_ACMEHostsAndCache(t *testing.T) {
	f := newTLSFiles(t)

	inst := Start(t, Config{Raw: acmeConfig("a.test", seedACMECache(t, f.ca, "a.test"), f.httpPort)})

	Until(t, Scale(reloadObserveTimeout), "first host to be served", func() bool {
		return acmeServes(inst, f.ca.CertPEM, "a.test")
	})

	reloadConfig(t, inst, acmeConfig("b.test", seedACMECache(t, f.ca, "b.test"), f.httpPort))

	Until(t, Scale(reloadObserveTimeout), "new host to be served", func() bool {
		return acmeServes(inst, f.ca.CertPEM, "b.test")
	})

	assert.False(t, acmeServes(inst, f.ca.CertPEM, "a.test"))

	status, location := redirectFrom(t, f.httpPort, "b.test")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://b.test/tiles/a?b=c", location)

	status, location = redirectFrom(t, f.httpPort, "a.test")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://b.test/tiles/a?b=c", location)
}

func Test_TLS_Reload_BetweenPEMAndACME(t *testing.T) {
	f := newTLSFiles(t)
	pem := pemProfile(f) + serverYAML(fmt.Sprintf(`
    tls: main
    httpPort: %v`, f.httpPort)) + tlsColorLayer

	inst := Start(t, Config{Raw: pem})
	client := httpsClient(t, f.ca.CertPEM)
	expectServed(t, inst, client)

	// Keeps the id so the unchanged server section still resolves
	reloadConfig(t, inst, strings.ReplaceAll(acmeConfig("a.test", seedACMECache(t, f.ca, "a.test"), f.httpPort), "public", "main"))

	Until(t, Scale(reloadObserveTimeout), "acme profile to be served", func() bool {
		return acmeServes(inst, f.ca.CertPEM, "a.test")
	})

	reloadConfig(t, inst, pem)

	Until(t, Scale(reloadObserveTimeout), "pem profile to be served", func() bool {
		status, err := getStatus(client, inst.httpsURL(tlsTilePath))
		return err == nil && status == http.StatusOK
	})
}

func Test_TLS_Reload_ACMEKeepsExistingConnections(t *testing.T) {
	f := newTLSFiles(t)
	cache := seedACMECache(t, f.ca, "a.test")

	inst := Start(t, Config{Raw: acmeConfig("a.test", cache, f.httpPort)})

	Until(t, Scale(reloadObserveTimeout), "host to be served", func() bool {
		return acmeServes(inst, f.ca.CertPEM, "a.test")
	})

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(f.ca.CertPEM)

	dialer := tls.Dialer{Config: &tls.Config{RootCAs: pool, ServerName: "a.test", MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}}

	conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(inst.ports.Server)))
	require.NoError(t, err)

	defer conn.Close()

	reloadConfig(t, inst, acmeConfig("b.test", seedACMECache(t, f.ca, "b.test"), f.httpPort))

	Until(t, Scale(reloadObserveTimeout), "new host to be served", func() bool {
		return acmeServes(inst, f.ca.CertPEM, "b.test")
	})

	_, err = fmt.Fprintf(conn, "GET %v HTTP/1.1\r\nHost: a.test\r\nConnection: close\r\n\r\n", tlsTilePath)
	require.NoError(t, err)

	body, err := io.ReadAll(conn)
	require.NoError(t, err)
	assert.Contains(t, string(body), "200 OK")
}

func Test_TLS_Reload_InvalidACMEKeepsServing(t *testing.T) {
	f := newTLSFiles(t)
	cache := seedACMECache(t, f.ca, "a.test")

	inst := Start(t, Config{Raw: acmeConfig("a.test", cache, f.httpPort)})

	Until(t, Scale(reloadObserveTimeout), "host to be served", func() bool {
		return acmeServes(inst, f.ca.CertPEM, "a.test")
	})

	reloadConfig(t, inst, acmeConfig("not a host", cache, f.httpPort))
	time.Sleep(Scale(settleWindow))

	assert.True(t, inst.Running())
	assert.True(t, acmeServes(inst, f.ca.CertPEM, "a.test"))
}

func Test_TLS_Deprecated_SurvivesReloadAndStopsRedirect(t *testing.T) {
	f := newTLSFiles(t)
	raw := serverYAML(fmt.Sprintf(`
    domain: localhost
    certificate: %v
    keyfile: %v
    httpPort: %v`, f.certFile, f.keyFile, f.httpPort)) + tlsColorLayer

	inst := Start(t, Config{Raw: raw})
	client := httpsClient(t, f.ca.CertPEM)
	expectServed(t, inst, client)

	reloadConfig(t, inst, raw)
	time.Sleep(Scale(settleWindow))

	expectServed(t, inst, client)

	status, location := redirectFrom(t, f.httpPort, "anything.test")
	assert.Equal(t, http.StatusMovedPermanently, status)
	assert.Equal(t, "https://localhost/tiles/a?b=c", location)

	inst.Signal(syscall.SIGTERM)
	assert.Equal(t, 0, inst.WaitExit(Scale(cleanupTimeout)))
}
