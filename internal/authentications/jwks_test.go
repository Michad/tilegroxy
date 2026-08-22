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
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jwksServer serves a keyset that tests can swap mid-run to simulate rotation, and can be made
// to fail to simulate an issuer outage.
type jwksServer struct {
	*httptest.Server
	mu       sync.Mutex
	body     string
	failing  bool
	requests int
}

func newJWKSServer(t *testing.T, body string) *jwksServer {
	t.Helper()

	s := &jwksServer{body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.requests++
		if s.failing {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		// No-store keeps the cache from serving a stale HTTP-cached copy across a rotation.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *jwksServer) setFailing(failing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failing = failing
}

func (s *jwksServer) setBody(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
}

func (s *jwksServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// rsaJWKS generates an RSA key and returns it alongside a single-key JWKS document.
func rsaJWKS(t *testing.T, kid, alg string) (*rsa.PrivateKey, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return key, jwksDocument(t, jwkEntry(t, key, kid, alg))
}

func jwkEntry(t *testing.T, key *rsa.PrivateKey, kid, alg string) map[string]any {
	t.Helper()

	return map[string]any{
		"kty": "RSA",
		"kid": kid,
		"alg": alg,
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}
}

func jwksDocument(t *testing.T, keys ...map[string]any) string {
	t.Helper()

	doc, err := json.Marshal(map[string]any{"keys": keys})
	require.NoError(t, err)

	return string(doc)
}

func testKeySet(t *testing.T, cfg JWKSConfig, algorithms []string) (*keySet, error) {
	t.Helper()

	ks, err := newKeySet(context.Background(), cfg, algorithms, config.ErrorMessages{})
	if ks != nil {
		t.Cleanup(func() { _ = ks.Close(context.Background()) })
	}

	return ks, err
}

func Test_NewKeySet_FetchesAtStartup(t *testing.T) {
	_, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})

	require.NoError(t, err)
	require.NotNil(t, ks)
	// A blocking first fetch is what makes a bad URL a startup failure rather than a 401 later.
	assert.Positive(t, server.requestCount())
}

func Test_NewKeySet_StartupFailureIsFatal(t *testing.T) {
	server := newJWKSServer(t, "")
	server.setFailing(true)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL, RequestTimeout: 1}, []string{"RS256"})

	require.Error(t, err)
	assert.Nil(t, ks)
}

func Test_NewKeySet_StartupFailureTolerated(t *testing.T) {
	server := newJWKSServer(t, "")
	server.setFailing(true)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL, RequestTimeout: 1, AllowStartupFailure: true}, []string{"RS256"})

	// Tolerating the failure has to yield a usable keySet, not a nil one, or every later request
	// panics instead of returning 401.
	require.NoError(t, err)
	require.NotNil(t, ks)
}

func Test_NewKeySet_Defaults(t *testing.T) {
	_, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})

	require.NoError(t, err)
	assert.Equal(t, uint(defaultRefreshInterval), ks.cfg.RefreshInterval)
	assert.Equal(t, uint(defaultRefreshMinInterval), ks.cfg.RefreshMinInterval)
	assert.Equal(t, uint(defaultRequestTimeout), ks.cfg.RequestTimeout)
}

func Test_ValidateURL(t *testing.T) {
	errorMessages := config.ErrorMessages{ParamRequired: "%s is required", InvalidParam: "%s is invalid: %s"}

	require.Error(t, validateURL("", errorMessages))
	require.Error(t, validateURL("not a url", errorMessages))
	require.Error(t, validateURL("http://example.com/jwks.json", errorMessages))
	require.NoError(t, validateURL("https://example.com/jwks.json", errorMessages))
}

func Test_KeyFor_MatchesKeyID(t *testing.T) {
	key, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	pub, err := ks.keyFor(context.Background(), "key-1")

	require.NoError(t, err)
	rsaPub, ok := pub.(*rsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, key.N, rsaPub.N)
}

func Test_KeyFor_UnknownKeyID(t *testing.T) {
	_, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	pub, err := ks.keyFor(context.Background(), "nonexistent")

	require.Error(t, err)
	assert.Nil(t, pub)
}

func Test_KeyFor_RotationRefetches(t *testing.T) {
	_, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	// RefreshMinInterval of 1 second keeps the test fast while still exercising the rate limit.
	ks, err := testKeySet(t, JWKSConfig{URL: server.URL, RefreshMinInterval: 1}, []string{"RS256"})
	require.NoError(t, err)

	newKey, newDoc := rsaJWKS(t, "key-2", "RS256")
	server.setBody(newDoc)

	// The rate limit floor has to elapse before a forced refresh is allowed through.
	time.Sleep(1100 * time.Millisecond)

	pub, err := ks.keyFor(context.Background(), "key-2")

	require.NoError(t, err)
	rsaPub, ok := pub.(*rsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, newKey.N, rsaPub.N)
}

func Test_KeyFor_AlgorithmNotAllowed(t *testing.T) {
	// The served key advertises RS512 but the operator only permits RS256.
	_, doc := rsaJWKS(t, "key-1", "RS512")
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	pub, err := ks.keyFor(context.Background(), "key-1")

	require.Error(t, err)
	assert.Nil(t, pub)
}

func Test_KeyFor_RejectsSymmetricKey(t *testing.T) {
	// A remote keyset handing back an HMAC key is the classic algorithm-confusion bypass: the
	// attacker signs a token using the public key as the shared secret.
	doc := jwksDocument(t, map[string]any{
		"kty": "oct",
		"kid": "key-1",
		"alg": "HS256",
		"k":   base64.RawURLEncoding.EncodeToString([]byte("hunter2")),
	})
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256", "HS256"})
	require.NoError(t, err)

	pub, err := ks.keyFor(context.Background(), "key-1")

	require.Error(t, err)
	assert.Nil(t, pub)
}

func Test_KeyFor_NoKeyIDSingleKey(t *testing.T) {
	key, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	// A token with no kid is only unambiguous when the keyset holds exactly one usable key.
	pub, err := ks.keyFor(context.Background(), "")

	require.NoError(t, err)
	rsaPub, ok := pub.(*rsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, key.N, rsaPub.N)
}

func Test_KeyFor_NoKeyIDMultipleKeys(t *testing.T) {
	first, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	second, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	doc := jwksDocument(t,
		jwkEntry(t, first, "key-1", "RS256"),
		jwkEntry(t, second, "key-2", "RS256"),
	)
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	pub, err := ks.keyFor(context.Background(), "")

	require.Error(t, err)
	assert.Nil(t, pub)
}

func Test_KeyFor_RejectsKeyWithoutAlgorithm(t *testing.T) {
	// alg is optional per RFC 7517; omitting it must still be rejected as the safe default.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	doc := jwksDocument(t, map[string]any{
		"kty": "RSA",
		"kid": "key-1",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	})
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	pub, err := ks.keyFor(context.Background(), "key-1")

	require.Error(t, err)
	assert.Nil(t, pub)
}

func Test_KeyFor_ExportsPublicHalfOfPrivateKey(t *testing.T) {
	// A well-behaved issuer never publishes private material, but keyFor must not be capable of
	// handing back a private key if a keyset ever contains one.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	doc := jwksDocument(t, map[string]any{
		"kty": "RSA",
		"kid": "key-1",
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		"d":   base64.RawURLEncoding.EncodeToString(key.D.Bytes()),
		"p":   base64.RawURLEncoding.EncodeToString(key.Primes[0].Bytes()),
		"q":   base64.RawURLEncoding.EncodeToString(key.Primes[1].Bytes()),
	})
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	pub, err := ks.keyFor(context.Background(), "key-1")

	require.NoError(t, err)
	_, isPrivate := pub.(*rsa.PrivateKey)
	assert.False(t, isPrivate)
	rsaPub, ok := pub.(*rsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, key.N, rsaPub.N)
}

func Test_KeyFor_ServesStaleWhenIssuerDown(t *testing.T) {
	_, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	ks, err := testKeySet(t, JWKSConfig{URL: server.URL}, []string{"RS256"})
	require.NoError(t, err)

	server.setFailing(true)

	// Keys fetched before the outage keep verifying; only genuinely new key IDs fail.
	pub, err := ks.keyFor(context.Background(), "key-1")

	require.NoError(t, err)
	assert.NotNil(t, pub)
}

// jwksJWT assembles a JWT authentication pointed at the test server, bypassing Initialize's
// https requirement since httptest serves over http.
func jwksJWT(t *testing.T, serverURL string) *JWT {
	t.Helper()

	cfg := JWTConfig{
		Algorithms:    []string{"RS256"},
		JWKS:          &JWKSConfig{URL: serverURL},
		MaxExpiration: 4294967295,
		HeaderName:    "Authorization",
		UserID:        "sub",
	}

	ks, err := testKeySet(t, *cfg.JWKS, cfg.Algorithms)
	require.NoError(t, err)

	return &JWT{JWTConfig: cfg, keys: ks, errorMessages: config.ErrorMessages{}}
}

func signedToken(t *testing.T, key *rsa.PrivateKey, kid string) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	require.NoError(t, err)

	return signed
}

func authRequest(t *testing.T, token string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, "http://localhost/tiles/layer/1/1/1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	return req
}

func Test_JWT_VerifiesTokenAgainstJWKS(t *testing.T) {
	key, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	auth := jwksJWT(t, server.URL)

	// CheckAuthentication needs a request-scoped context, not context.Background(), since it
	// writes results (like userID) through pointers the request context provides.
	assert.True(t, auth.CheckAuthentication(pkg.BackgroundContext(), authRequest(t, signedToken(t, key, "key-1"))))
}

func Test_JWT_RejectsTokenSignedByUnknownKey(t *testing.T) {
	_, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	auth := jwksJWT(t, server.URL)

	// Signed by a key the issuer never published, but claiming a kid the issuer does publish.
	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	assert.False(t, auth.CheckAuthentication(pkg.BackgroundContext(), authRequest(t, signedToken(t, attacker, "key-1"))))
}

func Test_JWT_Close_ReleasesJWKSCache(t *testing.T) {
	// This is what the shutdown path (AuthWrapper.Close via lifecycle.CloseIfCloser) reaches
	// for a JWKS-mode JWT auth.
	_, doc := rsaJWKS(t, "key-1", "RS256")
	server := newJWKSServer(t, doc)

	auth := jwksJWT(t, server.URL)

	assert.NoError(t, auth.Close(context.Background()))
}
