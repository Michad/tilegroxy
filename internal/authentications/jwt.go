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
	//TODO: Performance profile if the cache is actually worthwhile
	CacheSize        uint16 // Configures the size of the cache of already verified JWTs to avoid re-verifying keys for every token. Expiration still applies. Set to 0 to disable. Defaults to 0
	Key              string // The key for verifying the signature. The public key if using asymmetric signing. Required
	Algorithm        string // Algorithm to allow for JWT signature. Required
	HeaderName       string // The header to extract the JWT from. If this is "Authorization" it removes the "Bearer " from the start. Defaults to "Authorization"
	MaxExpiration    uint32 // How many seconds from now can the expiration be. JWTs more than X seconds from now will result in a 401. Defaults to 1 day
	ExpectedAudience string // If specified, require the "aud" grant to be this string
	ExpectedSubject  string // If specified, require the "sub" grant to be this string
	ExpectedIssuer   string // If specified, require the "iss" grant to be this string
	ExpectedScope    string // If specified, require the "scope" grant to contain this string.
	LayerScope       bool   // If specified, the "scope" grant is used to limit access to layer
	ScopePrefix      string // If LayerScope is true, this prefix indicates scopes to use
	UserID           string // Use the specified grant as the user identifier. Defaults to sub
}

type JWT struct {
	JWTConfig
	Cache         *otter.Cache[string, jwt.NumericDate]
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

func (s JWTRegistration) Initialize(configAny any, errorMessages config.ErrorMessages) (authentication.Authentication, error) {
	config := configAny.(JWTConfig)

	if !slices.Contains([]string{"HS256", "HS384", "HS512", "RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "PS256", "PS384", "PS512", "EdDSA"}, config.Algorithm) {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.algorithm", config.Algorithm)
	}

	if len(config.Key) < 1 {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.key", "")
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
		return &JWT{config, nil, errorMessages}, nil
	}

	cache, err := otter.MustBuilder[string, jwt.NumericDate](int(config.CacheSize)).Build()
	if err != nil {
		return nil, err
	}

	return &JWT{config, &cache, errorMessages}, nil
}

func (c JWT) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	tokenStr, ok := c.extractToken(req)
	if !ok {
		return false
	}

	if c.Cache != nil {
		date, ok := c.Cache.Get(tokenStr)

		if ok {
			slog.DebugContext(ctx, "JWT Cache hit")
			return date.After(time.Now())
		}
	}

	exp, ok := c.checkAuthenticationWithoutCache(tokenStr, ctx)
	if !ok {
		return false
	}

	if c.Cache != nil {
		c.Cache.SetIfAbsent(tokenStr, *exp)
	}

	return true
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

func (c JWT) checkAuthenticationWithoutCache(tokenStr string, ctx context.Context) (*jwt.NumericDate, bool) {
	parserOptions := make([]jwt.ParserOption, 0)
	parserOptions = append(parserOptions, jwt.WithLeeway(defaultLeeway))
	parserOptions = append(parserOptions, jwt.WithExpirationRequired())
	parserOptions = append(parserOptions, jwt.WithValidMethods([]string{c.Algorithm}))

	if len(c.ExpectedAudience) > 0 {
		parserOptions = append(parserOptions, jwt.WithAudience(c.ExpectedAudience))
	}
	if len(c.ExpectedSubject) > 0 {
		parserOptions = append(parserOptions, jwt.WithSubject(c.ExpectedSubject))
	}
	if len(c.ExpectedIssuer) > 0 {
		parserOptions = append(parserOptions, jwt.WithIssuer(c.ExpectedIssuer))
	}

	tokenJwt, err := jwt.Parse(tokenStr, c.parseKey, parserOptions...)

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
		validatePassed := c.validateScope(rawClaim, ctx)
		if !validatePassed {
			return nil, false
		}

		validatePassed = c.validateGeohash(rawClaim, ctx)
		if !validatePassed {
			return nil, false
		}

		rawUID := rawClaim[c.UserID]
		if rawUID != nil {
			ctxUserID, _ := pkg.UserIDFromContext(ctx)
			*ctxUserID, _ = rawUID.(string)
		}
	} else {
		return logInvalidClaimsType(tokenJwt, ctx)
	}
	return exp, true
}

func (c JWT) parseKey(_ *jwt.Token) (interface{}, error) {
	if strings.Index(c.Algorithm, "HS") == 0 {
		return []byte(c.Key), nil
	}
	if strings.Index(c.Algorithm, "RS") == 0 {
		return jwt.ParseRSAPublicKeyFromPEM([]byte(c.Key))
	}
	if strings.Index(c.Algorithm, "ES") == 0 {
		return jwt.ParseECPublicKeyFromPEM([]byte(c.Key))
	}
	if strings.Index(c.Algorithm, "PS") == 0 {
		return jwt.ParseRSAPublicKeyFromPEM([]byte(c.Key))
	}
	if c.Algorithm == "EdDSA" {
		return jwt.ParseEdPublicKeyFromPEM([]byte(c.Key))
	}

	return nil, fmt.Errorf(c.errorMessages.InvalidParam, "jwt.alg", c.Algorithm)
}

func logInvalidClaimsType(tokenJwt *jwt.Token, ctx context.Context) (*jwt.NumericDate, bool) {
	// notest

	var debugType string
	if t := reflect.TypeOf(tokenJwt.Claims); t.Kind() == reflect.Ptr {
		debugType = "*" + t.Elem().Name()
	} else {
		debugType = t.Name()
	}

	slog.ErrorContext(ctx, "An unexpected state has occurred. Please report this to https://github.com/Michad/tilegroxy/issues : JWT authentication might not be fully working as expected because claims are of type "+debugType)

	return nil, false
}

func (c JWT) validateScope(rawClaim jwt.MapClaims, ctx context.Context) bool {
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

func (c JWT) validateGeohash(rawClaim jwt.MapClaims, ctx context.Context) bool {
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

	return true
}
