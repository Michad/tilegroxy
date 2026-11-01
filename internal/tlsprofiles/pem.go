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
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

const (
	KindPEM = "pem"

	paramCertificateFile = "certificatefile"
	paramCertificate     = "certificate"
	paramKeyFile         = "keyfile"
	paramKey             = "key"
	paramCAFile          = "cafile"
	paramCA              = "ca"
	paramSystemCAs       = "systemcas"
	paramAllowedHosts    = "allowedhosts"
)

type PEMConfig struct {
	ID              string
	MinVersion      string
	CertificateFile string
	Certificate     string
	KeyFile         string
	Key             string
	CAFile          string
	CA              string
	SystemCAs       bool
	Hosts           []string
	AllowedHosts    []string
}

// A validated profile with its files already read
type PEM struct {
	profileBase

	Certificate  *tls.Certificate
	roots        *x509.CertPool
	clientCAs    *x509.CertPool
	allowedHosts []string
}

func init() {
	register(PEMRegistration{})
}

type PEMRegistration struct{}

func (PEMRegistration) InitializeConfig() any {
	return PEMConfig{}
}

func (PEMRegistration) Name() string {
	return KindPEM
}

func (PEMRegistration) Initialize(cfgAny any, deps Deps) (Profile, error) {
	cfg := cfgAny.(PEMConfig)

	base, err := newProfileBase(cfg.ID, cfg.MinVersion, deps)
	if err != nil {
		return nil, err
	}

	p := &PEM{profileBase: base}

	for param, value := range map[string]string{
		paramMinVersion:      cfg.MinVersion,
		paramCertificateFile: cfg.CertificateFile,
		paramCertificate:     cfg.Certificate,
		paramKeyFile:         cfg.KeyFile,
		paramKey:             cfg.Key,
		paramCAFile:          cfg.CAFile,
		paramCA:              cfg.CA,
	} {
		p.set[param] = value != ""
	}

	p.set[paramSystemCAs] = cfg.SystemCAs
	p.set[paramHosts] = len(cfg.Hosts) > 0
	p.set[paramAllowedHosts] = len(cfg.AllowedHosts) > 0

	if err := p.loadCertificate(cfg, deps); err != nil {
		return nil, err
	}

	if err := p.loadCAs(cfg, deps); err != nil {
		return nil, err
	}

	if err := p.loadHosts(cfg.Hosts, deps); err != nil {
		return nil, err
	}

	p.allowedHosts, err = normalizeAllowedHosts(cfg.AllowedHosts, deps.Path, deps.ErrorMessages)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (p *PEM) loadCertificate(cfg PEMConfig, deps Deps) error {
	path, messages := deps.Path, deps.ErrorMessages

	certPEM, err := readPEM(cfg.Certificate, cfg.CertificateFile, paramCertificate, paramCertificateFile, deps)
	if err != nil {
		return err
	}

	keyPEM, err := readPEM(cfg.Key, cfg.KeyFile, paramKey, paramKeyFile, deps)
	if err != nil {
		return err
	}

	if (len(certPEM) == 0) != (len(keyPEM) == 0) {
		return fmt.Errorf(messages.ParamsBothOrNeither, path+".certificate", path+".key")
	}

	if len(certPEM) == 0 {
		return nil
	}

	// The parse error is dropped since it could describe key material
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf(messages.InvalidParam, path+".certificate", "certificate and key are not a valid unencrypted PEM pair")
	}

	if cert.Leaf == nil {
		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return fmt.Errorf(messages.InvalidParam, path+".certificate", err)
		}
	}

	p.Certificate = &cert

	return nil
}

func (p *PEM) loadCAs(cfg PEMConfig, deps Deps) error {
	path, messages := deps.Path, deps.ErrorMessages

	caPEM, err := readPEM(cfg.CA, cfg.CAFile, paramCA, paramCAFile, deps)
	if err != nil {
		return err
	}

	if len(caPEM) == 0 {
		return nil
	}

	p.clientCAs = x509.NewCertPool()
	if !p.clientCAs.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf(messages.InvalidParam, path+".ca", "no certificates found")
	}

	p.roots = p.clientCAs

	if cfg.SystemCAs {
		p.roots, err = x509.SystemCertPool()
		if err != nil {
			return fmt.Errorf(messages.InvalidParam, path+".systemcas", err)
		}

		p.roots.AppendCertsFromPEM(caPEM)
	}

	return nil
}

func (p *PEM) loadHosts(hosts []string, deps Deps) error {
	path, messages := deps.Path, deps.ErrorMessages

	if len(hosts) > 0 && p.Certificate == nil {
		return fmt.Errorf(messages.ParamRequiresParam, path+".hosts", path+".certificate")
	}

	if p.Certificate == nil {
		return nil
	}

	if len(hosts) == 0 {
		for _, name := range p.Certificate.Leaf.DNSNames {
			p.Hosts = append(p.Hosts, normalizeHost(name))
		}

		return nil
	}

	for _, host := range hosts {
		normalized := normalizeHost(host)

		if !isHostOrIP(normalized) || p.Certificate.Leaf.VerifyHostname(normalized) != nil {
			return fmt.Errorf(messages.InvalidParam, path+".hosts", host)
		}

		p.Hosts = append(p.Hosts, normalized)
	}

	return nil
}

func readPEM(text, file, textParam, fileParam string, deps Deps) ([]byte, error) {
	path, messages := deps.Path, deps.ErrorMessages

	if text != "" && file != "" {
		return nil, fmt.Errorf(messages.ParamsMutuallyExclusive, path+"."+textParam, path+"."+fileParam)
	}

	if file == "" {
		return []byte(text), nil
	}

	content, err := os.ReadFile(file) // #nosec G304 -- path is operator configuration
	if err != nil {
		return nil, fmt.Errorf(messages.InvalidParam, path+"."+fileParam, err)
	}

	// Treating this as unset would make a truncated CA file silently trust the system CAs
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, fmt.Errorf(messages.InvalidParam, path+"."+fileParam, file)
	}

	return content, nil
}
