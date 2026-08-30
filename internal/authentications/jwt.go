// Copyright 2024 Michael Davis
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
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/golang-jwt/jwt/v5"
	"github.com/maypok86/otter"
)

const defaultExpiration = 24 * 60 * 60
const defaultLeeway = 5 * time.Second

type JWTConfig struct {
	CacheSize        uint16      // Configures the size of the cache of already verified JWTs to avoid re-verifying keys for every token. Expiration still applies. Set to 0 to disable. Defaults to 0
	Key              string      // The key for verifying the signature. The public key if using asymmetric signing. Required unless JWKS is supplied
	Algorithm        string      // Deprecated: use Algorithms. Retained so existing configurations keep working
	Algorithms       []string    // Algorithms to allow for JWT signatures. Required
	JWKS             *JWKSConfig // Fetch verification keys from a remote JWKS endpoint instead of Key
	HeaderName       string      // The header to extract the JWT from. If this is "Authorization" it removes the "Bearer " from the start. Defaults to "Authorization"
	MaxExpiration    uint32      // How many seconds from now can the expiration be. JWTs more than X seconds from now will result in a 401. Defaults to 1 day
	ExpectedAudience string      // If specified, require the "aud" grant to be this string
	ExpectedSubject  string      // If specified, require the "sub" grant to be this string
	ExpectedIssuer   string      // If specified, require the "iss" grant to be this string
	ExpectedScope    string      // If specified, require the "scope" grant to contain this string.
	LayerScope       bool        // If specified, the "scope" grant is used to limit access to layer
	ScopePrefix      string      // If LayerScope is true, this prefix indicates scopes to use
	UserID           string      // Use the specified grant as the user identifier. Defaults to sub
}

// cachedAuthResult holds everything CheckAuthentication derives from a validated token. A cache
// hit has to replay all of it, not just check expiration, or the restrictions the token carries
// are silently dropped.
type cachedAuthResult struct {
	expiration       jwt.NumericDate
	limitLayers      bool
	allowedLayers    []string
	limitAreaPartial bool
	allowedArea      pkg.Bounds
	userID           string
}

type JWT struct {
	JWTConfig
	Cache         *otter.Cache[string, cachedAuthResult]
	keys          *keySet
	errorMessages config.ErrorMessages
}

func init() {
	authentication.RegisterAuthentication(JWTRegistration{})
}

type JWTRegistration struct {
}

func (s JWTRegistration) InitializeConfig() any {
	return JWTConfig{}
}

func (s JWTRegistration) Name() string {
	return "jwt"
}

func (s JWTRegistration) Initialize(configAny any, deps authentication.AuthenticationDeps) (authentication.Authentication, error) {
	config := configAny.(JWTConfig)

	algorithms, err := resolveAlgorithms(config, deps.ErrorMessages)
	if err != nil {
		return nil, err
	}
	config.Algorithms = algorithms

	usingJWKS := config.JWKS != nil

	if usingJWKS && len(config.Key) > 0 {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "authentication.key", "authentication.jwks")
	}

	if !usingJWKS && len(config.Key) < 1 {
		return nil, fmt.Errorf(deps.ErrorMessages.OneOfRequired, []string{"authentication.key", "authentication.jwks.url"})
	}

	if !usingJWKS {
		// A static Key is one key of one type, so parseKey trusts algorithms[0] to pick the
		// parser. Mixing families would leave every algorithm but the first silently unusable.
		family := algorithmFamily(algorithms[0])
		for _, alg := range algorithms[1:] {
			if algorithmFamily(alg) != family {
				return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, algorithms[0], alg)
			}
		}
	}

	var keys *keySet
	if usingJWKS {
		if err = validateURL(config.JWKS.URL, deps.ErrorMessages); err != nil {
			return nil, err
		}

		// Symmetric keys from a remote keyset are the classic algorithm-confusion bypass.
		for _, alg := range algorithms {
			if strings.HasPrefix(alg, "HS") {
				return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "authentication.algorithms", alg)
			}
		}

		// context.Background() rather than a request context: the cache's goroutines must outlive
		// construction. Close is what stops them, via AuthWrapper on the shutdown path.
		keys, err = newKeySet(context.Background(), *config.JWKS, algorithms, deps.ErrorMessages)
		if err != nil {
			return nil, err
		}
	}

	if len(config.HeaderName) < 1 {
		config.HeaderName = "Authorization"
	}

	if config.MaxExpiration == 0 {
		config.MaxExpiration = defaultExpiration
	}

	if config.UserID == "" {
		config.UserID = "sub"
	}

	if config.CacheSize == 0 {
		return &JWT{JWTConfig: config, Cache: nil, keys: keys, errorMessages: deps.ErrorMessages}, nil
	}

	cache, err := otter.MustBuilder[string, cachedAuthResult](int(config.CacheSize)).Build()
	if err != nil {
		return nil, err
	}

	return &JWT{JWTConfig: config, Cache: &cache, keys: keys, errorMessages: deps.ErrorMessages}, nil
}

// resolveAlgorithms reconciles the deprecated single Algorithm against the Algorithms list.
// Setting both is an error rather than a precedence rule, so no operator has to guess which won.
func resolveAlgorithms(cfg JWTConfig, errorMessages config.ErrorMessages) ([]string, error) {
	if cfg.Algorithm != "" && len(cfg.Algorithms) > 0 {
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "authentication.algorithm", "authentication.algorithms")
	}

	algorithms := cfg.Algorithms
	if cfg.Algorithm != "" {
		algorithms = []string{cfg.Algorithm}
	}

	if len(algorithms) < 1 {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "authentication.algorithms")
	}

	validAlgorithms := []string{"HS256", "HS384", "HS512", "RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "PS256", "PS384", "PS512", "EdDSA"}
	for _, alg := range algorithms {
		if !slices.Contains(validAlgorithms, alg) {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.algorithms", alg)
		}
	}

	return algorithms, nil
}

// algorithmFamily maps an algorithm to the key type parseKey needs for it. RS and PS share a
// family since both parse as an RSA PEM public key.
func algorithmFamily(alg string) string {
	switch {
	case strings.HasPrefix(alg, "HS"):
		return "HS"
	case strings.HasPrefix(alg, "RS"), strings.HasPrefix(alg, "PS"):
		return "RSA"
	case strings.HasPrefix(alg, "ES"):
		return "ES"
	case alg == "EdDSA":
		return "EdDSA"
	default:
		return alg
	}
}

func (c JWT) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	tokenStr, ok := c.extractToken(req)
	if !ok {
		return false
	}

	if c.Cache != nil {
		result, ok := c.Cache.Get(tokenStr)

		if ok {
			if !result.expiration.After(time.Now()) {
				return false
			}

			slog.DebugContext(ctx, "JWT Cache hit")
			result.apply(ctx)
			return true
		}
	}

	result, ok := c.checkAuthenticationWithoutCache(ctx, tokenStr)
	if !ok {
		return false
	}

	if c.Cache != nil {
		c.Cache.SetIfAbsent(tokenStr, *result)
	}

	return true
}

// apply replays the authorization side effects a fresh validation would have set on the
// request context, so a cache hit behaves identically to a cache miss.
func (r cachedAuthResult) apply(ctx context.Context) {
	if ctxLimitLayers, ok := pkg.LimitLayersFromContext(ctx); ok {
		*ctxLimitLayers = r.limitLayers
	}
	if ctxAllowedLayers, ok := pkg.AllowedLayersFromContext(ctx); ok {
		*ctxAllowedLayers = append(*ctxAllowedLayers, r.allowedLayers...)
	}
	if ctxLimitAreaPartial, ok := pkg.LimitAreaPartialFromContext(ctx); ok {
		*ctxLimitAreaPartial = r.limitAreaPartial
	}
	if !r.allowedArea.IsNullIsland() {
		if ctxAllowedArea, ok := pkg.AllowedAreaFromContext(ctx); ok {
			*ctxAllowedArea = r.allowedArea
		}
	}
	if r.userID != "" {
		if ctxUserID, ok := pkg.UserIDFromContext(ctx); ok {
			*ctxUserID = r.userID
		}
	}
}

func (c JWT) extractToken(req *http.Request) (string, bool) {
	authHeader := req.Header[c.HeaderName]
	if len(authHeader) != 1 {
		return "", false
	}

	var tokenStr string
	if c.HeaderName == "Authorization" {
		tokenStr = strings.Replace(authHeader[0], "Bearer ", "", 1)
	} else {
		tokenStr = authHeader[0]
	}

	if len(tokenStr) < 1 {
		return "", false
	}
	return tokenStr, true
}

func (c JWT) checkAuthenticationWithoutCache(ctx context.Context, tokenStr string) (*cachedAuthResult, bool) {
	parserOptions := make([]jwt.ParserOption, 0)
	parserOptions = append(parserOptions, jwt.WithLeeway(defaultLeeway))
	parserOptions = append(parserOptions, jwt.WithExpirationRequired())
	parserOptions = append(parserOptions, jwt.WithValidMethods(c.Algorithms))

	if len(c.ExpectedAudience) > 0 {
		parserOptions = append(parserOptions, jwt.WithAudience(c.ExpectedAudience))
	}
	if len(c.ExpectedSubject) > 0 {
		parserOptions = append(parserOptions, jwt.WithSubject(c.ExpectedSubject))
	}
	if len(c.ExpectedIssuer) > 0 {
		parserOptions = append(parserOptions, jwt.WithIssuer(c.ExpectedIssuer))
	}

	tokenJwt, err := jwt.Parse(tokenStr, c.keyFunc(ctx), parserOptions...)

	if err != nil {
		slog.InfoContext(ctx, "JWT parsing error: "+err.Error())
		return nil, false
	}

	exp, err := tokenJwt.Claims.GetExpirationTime()

	if err != nil {
		return nil, false
	}

	if exp.Before(time.Now()) {
		return nil, false
	}

	if time.Until(exp.Time) > time.Duration(c.MaxExpiration)*time.Second {
		slog.InfoContext(ctx, "JWT parsing error: distant expiration")
		return nil, false
	}

	if c.LayerScope {
		ctxLimitLayers, _ := pkg.LimitLayersFromContext(ctx)
		*ctxLimitLayers = true
	}

	rawClaim, ok := tokenJwt.Claims.(jwt.MapClaims)

	if ok {
		validatePassed := c.validateScope(ctx, rawClaim)
		if !validatePassed {
			return nil, false
		}

		validatePassed = c.validateGeohash(ctx, rawClaim)
		if !validatePassed {
			return nil, false
		}

		rawUID := rawClaim[c.UserID]
		if rawUID != nil {
			ctxUserID, _ := pkg.UserIDFromContext(ctx)
			*ctxUserID, _ = rawUID.(string)
		}
	} else {
		return logInvalidClaimsType(ctx, tokenJwt)
	}

	result := cachedAuthResult{expiration: *exp}
	if ctxLimitLayers, ok := pkg.LimitLayersFromContext(ctx); ok {
		result.limitLayers = *ctxLimitLayers
	}
	if ctxAllowedLayers, ok := pkg.AllowedLayersFromContext(ctx); ok {
		result.allowedLayers = *ctxAllowedLayers
	}
	if ctxLimitAreaPartial, ok := pkg.LimitAreaPartialFromContext(ctx); ok {
		result.limitAreaPartial = *ctxLimitAreaPartial
	}
	if ctxAllowedArea, ok := pkg.AllowedAreaFromContext(ctx); ok {
		result.allowedArea = *ctxAllowedArea
	}
	if ctxUserID, ok := pkg.UserIDFromContext(ctx); ok {
		result.userID = *ctxUserID
	}

	return &result, true
}

func (c JWT) parseKey(_ *jwt.Token) (interface{}, error) {
	// Algorithms is always populated by resolveAlgorithms; the static key format is determined by
	// the first entry, since a single Key can only ever be one key type.
	alg := c.Algorithms[0]

	if strings.Index(alg, "HS") == 0 {
		return []byte(c.Key), nil
	}
	if strings.Index(alg, "RS") == 0 {
		return jwt.ParseRSAPublicKeyFromPEM([]byte(c.Key))
	}
	if strings.Index(alg, "ES") == 0 {
		return jwt.ParseECPublicKeyFromPEM([]byte(c.Key))
	}
	if strings.Index(alg, "PS") == 0 {
		return jwt.ParseRSAPublicKeyFromPEM([]byte(c.Key))
	}
	if alg == "EdDSA" {
		return jwt.ParseEdPublicKeyFromPEM([]byte(c.Key))
	}

	return nil, fmt.Errorf(c.errorMessages.InvalidParam, "jwt.alg", alg)
}

// keyFunc resolves the verification key. In JWKS mode it closes over the request context so a
// key fetch inherits its cancellation.
func (c JWT) keyFunc(ctx context.Context) jwt.Keyfunc {
	if c.keys == nil {
		return c.parseKey
	}

	return func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		return c.keys.keyFor(ctx, kid)
	}
}

// Close releases the JWKS cache and its background goroutines.
func (c JWT) Close(ctx context.Context) error {
	return c.keys.Close(ctx)
}

func logInvalidClaimsType(ctx context.Context, tokenJwt *jwt.Token) (*cachedAuthResult, bool) {
	// notest

	var debugType string
	if t := reflect.TypeOf(tokenJwt.Claims); t.Kind() == reflect.Pointer {
		debugType = "*" + t.Elem().Name()
	} else {
		debugType = t.Name()
	}

	slog.ErrorContext(ctx, "An unexpected state has occurred. Please report this to https://github.com/Michad/tilegroxy/issues : JWT authentication might not be fully working as expected because claims are of type "+debugType)

	return nil, false
}

func (c JWT) validateScope(ctx context.Context, rawClaim jwt.MapClaims) bool {
	scope := rawClaim["scope"]
	scopeStr, ok := scope.(string)

	if !ok {
		if scope != nil {
			slog.InfoContext(ctx, "Request contains invalid scope type")
		}

		if c.LayerScope || c.ExpectedScope != "" {
			return false
		}
	} else {
		scopeSplit := strings.Split(scopeStr, " ")

		if c.ExpectedScope != "" {
			hasScope := false
			for _, scope := range scopeSplit {
				if scope == c.ExpectedScope {
					hasScope = true
				}
			}
			if !hasScope {
				return false
			}
		}

		if c.LayerScope {
			ctxAllowedLayers, _ := pkg.AllowedLayersFromContext(ctx)
			for _, scope := range scopeSplit {
				if c.ScopePrefix == "" || strings.Index(scope, c.ScopePrefix) == 0 {
					*ctxAllowedLayers = append(*ctxAllowedLayers, scope[len(c.ScopePrefix):])
				}
			}
		}
	}

	return true
}

func (c JWT) validateGeohash(ctx context.Context, rawClaim jwt.MapClaims) bool {
	hash := rawClaim["geohash"]

	if hash == nil {
		return true
	}

	hashStr, ok := hash.(string)

	if !ok {
		slog.InfoContext(ctx, "Request contains invalid geohash type")
		return false
	}

	bounds, err := pkg.NewBoundsFromGeohash(hashStr)

	if err != nil {
		slog.InfoContext(ctx, "Request contains invalid geohash "+hashStr)
		return false
	}

	ctxAllowedArea, _ := pkg.AllowedAreaFromContext(ctx)
	*ctxAllowedArea = bounds
	ctxLimitAreaPartial, _ := pkg.LimitAreaPartialFromContext(ctx)
	*ctxLimitAreaPartial = true

	return true
}
