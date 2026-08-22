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

package authentications

import (
	"context"
	"crypto"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

const defaultRefreshInterval = 900
const defaultRefreshMinInterval = 60
const defaultRequestTimeout = 10

type JWKSConfig struct {
	URL                 string // The HTTPS URL of the JWKS endpoint. Required
	RefreshInterval     uint   // How many seconds between unconditional refreshes of the keyset. Defaults to 900
	RefreshMinInterval  uint   // The minimum seconds between refreshes triggered by an unknown key ID. Defaults to 60
	RequestTimeout      uint   // How many seconds before a fetch of the keyset is cancelled. Defaults to 10
	AllowStartupFailure bool   // If true, a failed fetch at startup logs a warning instead of preventing startup. Defaults to false
}

type keySet struct {
	cfg           JWKSConfig
	algorithms    []string
	cache         *jwk.Cache
	errorMessages config.ErrorMessages
}

func newKeySet(ctx context.Context, cfg JWKSConfig, algorithms []string, errorMessages config.ErrorMessages) (*keySet, error) {
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = defaultRefreshInterval
	}
	if cfg.RefreshMinInterval == 0 {
		cfg.RefreshMinInterval = defaultRefreshMinInterval
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}

	httpClient := &http.Client{Timeout: time.Duration(cfg.RequestTimeout) * time.Second}
	cache, err := jwk.NewCache(ctx, httprc.NewClient(httprc.WithHTTPClient(httpClient)))
	if err != nil {
		return nil, err
	}

	k := &keySet{cfg: cfg, algorithms: algorithms, cache: cache, errorMessages: errorMessages}

	// Register performs the first fetch and blocks on it, so it gets an explicit deadline on top
	// of the client timeout. Refresh needs no such bound because the resource is already
	// registered, so it returns promptly on failure rather than retrying.
	registerCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.RequestTimeout)*time.Second)
	err = cache.Register(registerCtx, cfg.URL,
		jwk.WithMinInterval(time.Duration(cfg.RefreshMinInterval)*time.Second),
		jwk.WithConstantInterval(time.Duration(cfg.RefreshInterval)*time.Second),
	)
	cancel()
	if err != nil {
		if startupErr := k.startupError(ctx, err); startupErr != nil {
			return nil, startupErr
		}
		return k, nil
	}

	// Fetch once now so a bad URL fails at startup rather than surfacing as 401s later.
	if _, err = cache.Refresh(ctx, cfg.URL); err != nil {
		if startupErr := k.startupError(ctx, err); startupErr != nil {
			return nil, startupErr
		}
	}

	return k, nil
}

func (k *keySet) startupError(ctx context.Context, err error) error {
	if !k.cfg.AllowStartupFailure {
		return fmt.Errorf(k.errorMessages.InvalidParam, "authentication.jwks.url", k.cfg.URL+": "+err.Error())
	}

	slog.WarnContext(ctx, "Could not fetch JWKS at startup, continuing because allowStartupFailure is set: "+err.Error())
	return nil
}

func (k *keySet) keyFor(ctx context.Context, kid string) (crypto.PublicKey, error) {
	set, err := k.cache.Lookup(ctx, k.cfg.URL)
	if err != nil {
		return nil, err
	}

	key, found := k.lookup(set, kid)

	// An unknown key ID usually means the issuer rotated, so force one refresh before rejecting.
	// The cache rate limits this to RefreshMinInterval, which stops a flood of bogus key IDs
	// from turning into a flood of outbound requests.
	if !found {
		refreshed, refreshErr := k.cache.Refresh(ctx, k.cfg.URL)
		if refreshErr != nil {
			return nil, refreshErr
		}

		key, found = k.lookup(refreshed, kid)
	}

	if !found {
		return nil, fmt.Errorf(k.errorMessages.InvalidParam, "jwt.kid")
	}

	if err = k.checkKeyAlgorithm(key); err != nil {
		return nil, err
	}

	pubKey, err := key.PublicKey()
	if err != nil {
		return nil, err
	}

	var pub any
	if err = jwk.Export(pubKey, &pub); err != nil {
		return nil, err
	}

	return pub, nil
}

// lookup finds the key by ID, or the only key in the set when the token carries no kid. More
// than one candidate with no kid is ambiguous and rejected rather than guessed at.
func (k *keySet) lookup(set jwk.Set, kid string) (jwk.Key, bool) {
	if kid != "" {
		return set.LookupKeyID(kid)
	}

	if set.Len() != 1 {
		return nil, false
	}

	return set.Key(0)
}

func (k *keySet) checkKeyAlgorithm(key jwk.Key) error {
	if key.KeyType() == jwa.OctetSeq() {
		return fmt.Errorf(k.errorMessages.InvalidParam, "authentication.jwks.key.alg", key.KeyType().String())
	}

	alg, ok := key.Algorithm()
	if !ok {
		return fmt.Errorf(k.errorMessages.ParamRequired, "authentication.jwks.key.alg")
	}

	if !slices.Contains(k.algorithms, alg.String()) {
		return fmt.Errorf(k.errorMessages.InvalidParam, "authentication.jwks.key.alg", alg.String())
	}

	return nil
}

func (k *keySet) Close(ctx context.Context) error {
	if k == nil || k.cache == nil {
		return nil
	}

	return k.cache.Shutdown(ctx)
}

func validateURL(rawURL string, errorMessages config.ErrorMessages) error {
	if rawURL == "" {
		return fmt.Errorf(errorMessages.ParamRequired, "authentication.jwks.url")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf(errorMessages.InvalidParam, "authentication.jwks.url", rawURL)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf(errorMessages.InvalidParam, "authentication.jwks.url", rawURL)
	}

	return nil
}
