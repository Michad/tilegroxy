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

package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	certValidity = 24 * time.Hour
	serialBits   = 62
	testFileMode = 0600
)

type CA struct {
	CertPEM []byte
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
}

// Usable by both sides of a connection
type Leaf struct {
	CertPEM []byte
	KeyPEM  []byte
}

func NewCA(t testing.TB, name string) *CA {
	t.Helper()

	key := newKey(t)
	template := certTemplate(t, name)
	template.IsCA = true
	template.BasicConstraintsValid = true
	template.KeyUsage = x509.KeyUsageCertSign

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cannot create CA: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("cannot parse CA: %v", err)
	}

	return &CA{CertPEM: encodePEM("CERTIFICATE", der), cert: cert, key: key}
}

// Each host is added as an IP or DNS name as appropriate
func (c *CA) Issue(t testing.TB, hosts ...string) Leaf {
	t.Helper()

	key := newKey(t)
	template := certTemplate(t, "leaf")
	template.KeyUsage = x509.KeyUsageDigitalSignature
	template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}

	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, c.cert, &key.PublicKey, c.key)
	if err != nil {
		t.Fatalf("cannot create certificate: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("cannot marshal key: %v", err)
	}

	return Leaf{CertPEM: encodePEM("CERTIFICATE", der), KeyPEM: encodePEM("EC PRIVATE KEY", keyDER)}
}

// Writes into a test temp directory and returns the path
func WriteTempFile(t testing.TB, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, testFileMode); err != nil {
		t.Fatalf("cannot write %v: %v", name, err)
	}

	return path
}

func newKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}

	return key
}

func certTemplate(t testing.TB, name string) *x509.Certificate {
	t.Helper()

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<serialBits))
	if err != nil {
		t.Fatalf("cannot generate serial: %v", err)
	}

	return &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(certValidity),
	}
}

func encodePEM(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}
