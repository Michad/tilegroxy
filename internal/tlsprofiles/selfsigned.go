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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"time"
)

const (
	KindSelfSigned = "selfsigned"

	selfSignedValidity   = 365 * 24 * time.Hour
	selfSignedClockSkew  = time.Hour
	selfSignedSerialBits = 128
)

type SelfSignedConfig struct {
	ID         string
	MinVersion string
	Hosts      []string
}

// Holds a certificate generated during construction and kept only in memory
type SelfSigned struct {
	profileBase

	Certificate *tls.Certificate
}

func init() {
	register(SelfSignedRegistration{})
}

type SelfSignedRegistration struct{}

func (SelfSignedRegistration) InitializeConfig() any {
	return SelfSignedConfig{}
}

func (SelfSignedRegistration) Name() string {
	return KindSelfSigned
}

func (SelfSignedRegistration) Initialize(cfgAny any, deps Deps) (Profile, error) {
	cfg := cfgAny.(SelfSignedConfig)

	base, err := newProfileBase(cfg.ID, cfg.MinVersion, deps)
	if err != nil {
		return nil, err
	}

	p := &SelfSigned{profileBase: base}

	p.set[paramMinVersion] = cfg.MinVersion != ""
	p.set[paramHosts] = len(cfg.Hosts) > 0

	hosts := cfg.Hosts
	if len(hosts) == 0 {
		hosts = []string{"localhost", "127.0.0.1", "::1"}
	}

	for _, host := range hosts {
		normalized := normalizeHost(host)
		if !isHostOrIP(normalized) {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, deps.Path+".hosts", host)
		}

		p.Hosts = append(p.Hosts, normalized)
	}

	p.Certificate, err = generateSelfSigned(p.Hosts)
	if err != nil {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, deps.Path, err)
	}

	return p, nil
}

func generateSelfSigned(hosts []string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), selfSignedSerialBits))
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "tilegroxy self-signed"},
		NotBefore:             now.Add(-selfSignedClockSkew),
		NotAfter:              now.Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}
