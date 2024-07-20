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
	"os"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/maypok86/otter"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"k8s.io/utils/keymutex"
)

const (
	ExtractModeHeader     = "header"
	ExtractModeCookie     = "cookie"
	ExtractModeQuery      = "query"
	ExtractModePathSuffix = "path"
)

type CustomConfig struct {
	Token     map[string]string //How to extract the auth token from the request. Key should be one of the ExtractMode and Value is the specific identifier (or blank if inapplicable)
	CacheSize int               //Configures the size of the cache of already verified tokens to avoid re-verifying every request. Set to -1 to disable. Defaults to 100
	File      string            //Contains the go code to perform validation of the auth token as a file.
	Script    string            //Contains the go code to perform validation of the auth token inline.
}

type Custom struct {
	CustomConfig
	cache *otter.Cache[string, ValidationResult]
	//Only used when cache is used to avoid multiple calls to the validation func for the same token at once
	locks          keymutex.KeyMutex
	validationFunc func(string) (bool, time.Time, string, []string)
}

type ValidationResult struct {
	pass       bool
	expiration time.Time
	uid        string
	layers     []string
}

func toResult(pass bool, exp time.Time, uid string, layers []string) ValidationResult {
	return ValidationResult{pass, exp, uid, layers}
}

func (v ValidationResult) isGood() bool {
	if !v.pass {
		return false
	}

	if v.expiration.Before(time.Now()) {
		return false
	}

	return true
}

func extractToken(req *http.Request, ctx *pkg.RequestContext, tokenExtract map[string]string) (string, bool) {
	h, hOk := tokenExtract[ExtractModeHeader]
	c, cOk := tokenExtract[ExtractModeCookie]
	q, qOk := tokenExtract[ExtractModeQuery]
	_, pOk := tokenExtract[ExtractModePathSuffix]

	if hOk {
		hToken, ok := req.Header[h]
		if ok && len(hToken) > 0 {
			return hToken[0], true
		}
	}

	if cOk {
		cookie, err := req.Cookie(c)
		if err != nil {
			slog.DebugContext(ctx, "Custom auth cookie error: "+err.Error())
		} else if cookie != nil {
			//TODO: Do we want to enforce restrictions on flags? E.g. ignore cookies without HttpOnly set
			return cookie.Value, true
		}
	}

	if qOk {
		qVal := req.URL.Query()
		if qVal.Has(q) {
			return qVal.Get(q), true
		}
	}

	if pOk {
		pathSplit := strings.Split(req.URL.Path, "/")

		lastVal := pathSplit[len(pathSplit)-1]

		//This is a little hacky, make sure we're getting a suffix and not just the last tile coordinate. Surely the token won't be an integer
		yVal := req.PathValue("y")
		if yVal != lastVal {
			return lastVal, true
		}
	}

	return "", false
}

func init() {
	entities.Register(entities.EntityAuth, CustomRegistration{})
}

type CustomRegistration struct {
}

func (s CustomRegistration) InitializeConfig() any {
	return CustomConfig{}
}

func (s CustomRegistration) Name() string {
	return "custom"
}

func (s CustomRegistration) Initialize(cfgAny any, errorMessages config.ErrorMessages) (Authentication, error) {
	cfg := cfgAny.(CustomConfig)
	var err error

	if cfg.Token == nil || len(cfg.Token) == 0 {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "auth.custom.tokenextract", cfg.Token)
	}

	i := interp.New(interp.Options{Unrestricted: true})
	i.Use(stdlib.Symbols)

	var script string

	if cfg.File != "" {
		scriptBytes, err := os.ReadFile(cfg.File)
		if err != nil {
			return nil, err
		}
		script = string(scriptBytes)
	} else {
		script = cfg.Script
	}

	_, err = i.Eval(script)
	if err != nil {
		return nil, fmt.Errorf(errorMessages.ScriptError, "auth.custom", err)
	}

	validationVal, err := i.Eval("custom.validate")
	if err != nil {
		return nil, fmt.Errorf(errorMessages.ScriptError, "auth.custom", err)
	}
	if validationVal.IsNil() {
		return nil, fmt.Errorf(errorMessages.ScriptError, "auth.custom", "nil")
	}

	validationFunc, ok := validationVal.Interface().(func(string) (bool, time.Time, string, []string))

	if !ok {
		return nil, fmt.Errorf(errorMessages.ScriptError, "auth.custom", validationVal)
	}

	if cfg.CacheSize == 0 {
		cfg.CacheSize = 100
	}

	if cfg.CacheSize < 0 {
		return &Custom{cfg, nil, nil, validationFunc}, nil
	} else {
		lock := keymutex.NewHashed(-1)

		cache, err := otter.MustBuilder[string, ValidationResult](int(cfg.CacheSize)).Build()
		if err != nil {
			return nil, err
		}

		return &Custom{cfg, &cache, lock, validationFunc}, nil
	}
}

func (c Custom) CheckAuthentication(req *http.Request, ctx *pkg.RequestContext) bool {
	slog.Log(ctx, config.LevelTrace, "Performing custom auth check")
	tok, ok := extractToken(req, ctx, c.Token)
	if ok {
		var valResult ValidationResult
		var inCache bool

		if c.cache != nil {
			c.locks.LockKey(tok)
			defer c.locks.UnlockKey(tok)

			valResult, inCache = c.cache.Get(tok)
			if !inCache {
				slog.Log(ctx, config.LevelTrace, "Cache miss")
			}
		}

		if !inCache {
			valResult = toResult(c.validationFunc(tok))
			slog.DebugContext(ctx, fmt.Sprintf("Custom auth check returned %v", valResult.pass))

			if c.cache != nil {
				c.cache.Set(tok, valResult)
			}
		}

		if valResult.isGood() {
			ctx.UserIdentifier = valResult.uid

			if len(valResult.layers) > 0 {
				ctx.LimitLayers = true
				ctx.AllowedLayers = valResult.layers
			}
			slog.Log(ctx, config.LevelTrace, "Custom auth passed", "result", valResult)

			return true
		}
	} else {
		slog.Log(ctx, config.LevelTrace, "Request lacked any auth token")
	}

	return false
}
