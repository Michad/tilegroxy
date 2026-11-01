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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const (
	KindACME = "acme"

	defaultACMECache = "./certs"

	paramCache = "cache"
)

type ACMEConfig struct {
	ID         string
	MinVersion string
	Hosts      []string
	Cache      string
}

// Obtains its certificates on demand through a manager held until Close
type ACME struct {
	profileBase

	Cache   string
	manager *ACMEManager
}

func init() {
	register(ACMERegistration{})
}

type ACMERegistration struct{}

func (ACMERegistration) InitializeConfig() any {
	return ACMEConfig{}
}

func (ACMERegistration) Name() string {
	return KindACME
}

func (ACMERegistration) Initialize(cfgAny any, deps Deps) (Profile, error) {
	cfg := cfgAny.(ACMEConfig)

	base, err := newProfileBase(cfg.ID, cfg.MinVersion, deps)
	if err != nil {
		return nil, err
	}

	p := &ACME{profileBase: base, Cache: cfg.Cache}

	p.set[paramMinVersion] = cfg.MinVersion != ""
	p.set[paramHosts] = true
	p.set[paramCache] = cfg.Cache != ""

	if len(cfg.Hosts) == 0 {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, deps.Path+".hosts")
	}

	for _, host := range cfg.Hosts {
		normalized := normalizeHost(host)
		if !isHostname(normalized) || net.ParseIP(normalized) != nil {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, deps.Path+".hosts", host)
		}

		p.Hosts = append(p.Hosts, normalized)
	}

	if p.Cache == "" {
		p.Cache = defaultACMECache
	}

	return p, nil
}

func (p *ACME) Manager() *ACMEManager {
	if p.manager == nil {
		p.manager = AcquireACME(p.Hosts, p.Cache)
	}

	return p.manager
}

func (p *ACME) Close(_ context.Context) error {
	if p.manager != nil {
		p.manager.Release()
		p.manager = nil
	}

	return nil
}

var errACMEClosed = errors.New("acme manager was released")

// autocert can't stop renewal timers, so generations with unchanged settings share a manager instead of accumulating them
var (
	acmeManagersMu sync.Mutex
	acmeManagers   = map[string]*ACMEManager{}
)

// A reference-counted certificate manager, shared by every generation with the same hosts and cache
type ACMEManager struct {
	manager *autocert.Manager
	key     string
	refs    int
	closed  atomic.Bool
}

// Every call needs a matching Release
func AcquireACME(hosts []string, cache string) *ACMEManager {
	sorted := slices.Sorted(slices.Values(hosts))
	key := cache + "\n" + strings.Join(sorted, "\n")

	acmeManagersMu.Lock()
	defer acmeManagersMu.Unlock()

	m, ok := acmeManagers[key]
	if !ok {
		m = &ACMEManager{key: key}
		m.manager = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(hosts...),
			Cache:      gatedCache{autocert.DirCache(cache), &m.closed},
			Client: &acme.Client{
				DirectoryURL: autocert.DefaultACMEDirectory,
				HTTPClient:   &http.Client{Transport: gatedTransport{&m.closed}},
			},
		}
		acmeManagers[key] = m
	}

	m.refs++

	return m
}

// The last release leaves any renewal timers unable to reach the cache or the ACME server
func (m *ACMEManager) Release() {
	acmeManagersMu.Lock()
	defer acmeManagersMu.Unlock()

	m.refs--
	if m.refs == 0 {
		m.closed.Store(true)
		delete(acmeManagers, m.key)
	}
}

func (m *ACMEManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.manager.GetCertificate(hello)
}

func (m *ACMEManager) HTTPHandler(fallback http.Handler) http.Handler {
	return m.manager.HTTPHandler(fallback)
}

type gatedTransport struct {
	closed *atomic.Bool
}

func (t gatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.closed.Load() {
		return nil, errACMEClosed
	}

	return http.DefaultTransport.RoundTrip(req)
}

type gatedCache struct {
	autocert.Cache
	closed *atomic.Bool
}

func (c gatedCache) Get(ctx context.Context, name string) ([]byte, error) {
	if c.closed.Load() {
		return nil, errACMEClosed
	}

	return c.Cache.Get(ctx, name)
}

func (c gatedCache) Put(ctx context.Context, name string, data []byte) error {
	if c.closed.Load() {
		return errACMEClosed
	}

	return c.Cache.Put(ctx, name, data)
}

func (c gatedCache) Delete(ctx context.Context, name string) error {
	if c.closed.Load() {
		return errACMEClosed
	}

	return c.Cache.Delete(ctx, name)
}
