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

package authentication

import (
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/maypok86/otter"
)

type JwtConfig struct {
	//TODO: Performance profile if the cache is actually worthwhile
	CacheSize        uint16 //Configures the size of the cache of already verified JWTs to avoid re-verifying keys for every token. Expiration still applies. Set to 0 to disable. Defaults to 0
	Key              string //The key for verifying the signature. The public key if using asymmetric signing. Required
	Algorithm        string //Algorithm to allow for JWT signature. Required
	HeaderName       string //The header to extract the JWT from. If this is "Authorization" it removes the "Bearer " from the start. Defaults to "Authorization"
	MaxExpiration    uint32 //How many seconds from now can the expiration be. JWTs more than X seconds from now will result in a 401. Defaults to 1 day
	ExpectedAudience string //If specified, require the "aud" grant to be this string
	ExpectedSubject  string //If specified, require the "sub" grant to be this string
	ExpectedIssuer   string //If specified, require the "iss" grant to be this string
	ExpectedScope    string //If specified, require the "scope" grant to contain this string.
	LayerScope       bool   //If specified, the "scope" grant is used to limit access to layer
	ScopePrefix      string //If LayerScope is true, this prefix indicates scopes to use
	UserId           string //Use the specified grant as the user identifier. Defaults to sub
}

type Jwt struct {
	Config        *JwtConfig
	Cache         *otter.Cache[string, jwt.NumericDate]
	errorMessages *config.ErrorMessages
}

func ConstructJwt(config *JwtConfig, errorMessages *config.ErrorMessages) (*Jwt, error) {
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
		config.MaxExpiration = 24 * 60 * 60
	}

	if config.UserId == "" {
		config.UserId = "sub"
	}

	if config.CacheSize == 0 {
		return &Jwt{config, nil, errorMessages}, nil
	} else {
		cache, err := otter.MustBuilder[string, jwt.NumericDate](int(config.CacheSize)).Build()
		if err != nil {
			return nil, err
		}

		return &Jwt{config, &cache, errorMessages}, nil
	}
}

func (c Jwt) CheckAuthentication(req *http.Request, ctx *internal.RequestContext) bool {
	authHeader := req.Header[c.Config.HeaderName]
	if len(authHeader) != 1 {
		return false
	}

	var tokenStr string
	if c.Config.HeaderName == "Authorization" {
		tokenStr = strings.Replace(authHeader[0], "Bearer ", "", 1)
	} else {
		tokenStr = authHeader[0]
	}

	if len(tokenStr) < 1 {
		return false
	}

	if c.Cache != nil {
		date, ok := c.Cache.Get(tokenStr)

		if ok {
			slog.DebugContext(ctx, "JWT Cache hit")
			if date.After(time.Now()) {
				return true
			} else {
				return false
			}
		}
	}

	parserOptions := make([]jwt.ParserOption, 0)
	parserOptions = append(parserOptions, jwt.WithLeeway(5*time.Second))
	parserOptions = append(parserOptions, jwt.WithExpirationRequired())
	parserOptions = append(parserOptions, jwt.WithValidMethods([]string{c.Config.Algorithm}))

	if len(c.Config.ExpectedAudience) > 0 {
		parserOptions = append(parserOptions, jwt.WithAudience(c.Config.ExpectedAudience))
	}
	if len(c.Config.ExpectedSubject) > 0 {
		parserOptions = append(parserOptions, jwt.WithSubject(c.Config.ExpectedSubject))
	}
	if len(c.Config.ExpectedIssuer) > 0 {
		parserOptions = append(parserOptions, jwt.WithIssuer(c.Config.ExpectedIssuer))
	}

	tokenJwt, error := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if strings.Index(c.Config.Algorithm, "HS") == 0 {
			return []byte(c.Config.Key), nil
		}
		if strings.Index(c.Config.Algorithm, "RS") == 0 {
			return jwt.ParseRSAPublicKeyFromPEM([]byte(c.Config.Key))
		}
		if strings.Index(c.Config.Algorithm, "ES") == 0 {
			return jwt.ParseECPublicKeyFromPEM([]byte(c.Config.Key))
		}
		if strings.Index(c.Config.Algorithm, "PS") == 0 {
			return jwt.ParseRSAPublicKeyFromPEM([]byte(c.Config.Key))
		}
		if c.Config.Algorithm == "EdDSA" {
			return jwt.ParseEdPublicKeyFromPEM([]byte(c.Config.Key))
		}

		return nil, fmt.Errorf(c.errorMessages.InvalidParam, "jwt.alg", c.Config.Algorithm)
	}, parserOptions...)

	if error != nil {
		slog.InfoContext(ctx, "JWT parsing error: ", error)
		return false
	}

	exp, error := tokenJwt.Claims.GetExpirationTime()

	if error != nil {
		return false
	}

	if exp.Before(time.Now()) {
		return false
	}

	if time.Until(exp.Time) > time.Duration(c.Config.MaxExpiration)*time.Second {
		slog.InfoContext(ctx, "JWT parsing error: distant expiration")
		return false
	}

	if c.Config.LayerScope {
		ctx.LimitLayers = true
	}

	rawClaim, ok := tokenJwt.Claims.(jwt.MapClaims)

	if ok {
		scope := rawClaim["scope"]

		scopeStr, ok := scope.(string)

		if !ok {
			slog.InfoContext(ctx, "Request contains invalid scope type")
		} else {
			scopeSplit := strings.Split(scopeStr, " ")

			if c.Config.ExpectedScope != "" {
				hasScope := false
				for _, scope := range scopeSplit {
					if scope == c.Config.ExpectedScope {
						hasScope = true
					}
				}
				if !hasScope {
					return false
				}
			}

			if c.Config.LayerScope {
				for _, scope := range scopeSplit {
					if c.Config.ScopePrefix == "" || strings.Index(scope, c.Config.ScopePrefix) == 0 {
						ctx.AllowedLayers = append(ctx.AllowedLayers, scope[len(c.Config.ScopePrefix):])
					}
				}
			}
		}

		rawUid := rawClaim[c.Config.UserId]
		if rawUid != nil {
			ctx.UserIdentifier, _ = rawUid.(string)
		}
	} else {
		var debugType string
		if t := reflect.TypeOf(tokenJwt.Claims); t.Kind() == reflect.Ptr {
			debugType = "*" + t.Elem().Name()
		} else {
			debugType = t.Name()
		}

		slog.ErrorContext(ctx, "An unexpected state has occurred. Please report this to https://github.com/Michad/tilegroxy/issues : JWT authentication might not be fully working as expected because claims are of type "+debugType)

		if c.Config.ExpectedScope != "" {
			return false
		}
	}

	if c.Cache != nil {
		c.Cache.SetIfAbsent(tokenStr, *exp)
	}

	return true
}
