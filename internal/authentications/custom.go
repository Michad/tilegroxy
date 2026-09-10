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
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
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
	Token     map[string]string // How to extract the auth token from the request. Key should be one of the ExtractMode and Value is the specific identifier (or blank if inapplicable)
	CacheSize int               // Configures the size of the cache of already verified tokens to avoid re-verifying every request. Set to -1 to disable. Defaults to 100
	File      string            // Contains the go code to perform validation of the auth token as a file.
	Script    string            // Contains the go code to perform validation of the auth token inline.
}

type Custom struct {
	CustomConfig
	cache *otter.Cache[string, authentication.ValidationResult]
	// Only used when cache is used to avoid multiple calls to the validation func for the same token at once
	locks          keymutex.KeyMutex
	validationFunc func(string) authentication.ValidationResult
	closeFunc      func(context.Context) error
}

func checkValidationResult(v authentication.ValidationResult) bool {
	if !v.Pass {
		return false
	}

	return !v.Expiration.Before(time.Now())
}

func extractToken(ctx context.Context, req *http.Request, tokenExtract map[string]string) (string, bool) {
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

		// This is a little hacky, make sure we're getting a suffix and not just the last tile coordinate. Surely the token won't be an integer
		yVal := req.PathValue("y")
		if yVal != lastVal {
			return lastVal, true
		}
	}

	return "", false
}

func init() {
	authentication.RegisterAuthentication(CustomRegistration{})
}

type CustomRegistration struct {
}

func (s CustomRegistration) InitializeConfig() any {
	return CustomConfig{}
}

func (s CustomRegistration) Name() string {
	return "custom"
}

func (s CustomRegistration) Initialize(cfgAny any, deps authentication.AuthenticationDeps) (authentication.Authentication, error) {
	cfg := cfgAny.(CustomConfig)
	var err error

	if len(cfg.Token) == 0 {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "auth.custom.tokenextract", cfg.Token)
	}

	i := interp.New(interp.Options{Unrestricted: true})
	err = i.Use(stdlib.Symbols)
	if err != nil {
		return nil, err
	}

	err = i.Use(interp.Exports{
		"tilegroxy/tilegroxy": map[string]reflect.Value{
			"ValidationResult": reflect.ValueOf((*authentication.ValidationResult)(nil)),
		},
	})
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf(deps.ErrorMessages.ScriptError, "auth.custom", err)
	}

	validationVal, err := i.Eval("custom.validate")
	if err != nil {
		return nil, fmt.Errorf(deps.ErrorMessages.ScriptError, "auth.custom", err)
	}
	if validationVal.IsNil() {
		return nil, fmt.Errorf(deps.ErrorMessages.ScriptError, "auth.custom", "nil")
	}

	validationFunc, ok := validationVal.Interface().(func(string) authentication.ValidationResult)

	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.ScriptError, "auth.custom", validationVal)
	}

	// close is optional so scripts written before it existed keep working unchanged.
	var closeFunc func(context.Context) error
	if closeVal, closeErr := i.Eval("custom.close"); closeErr == nil {
		fn, ok := closeVal.Interface().(func(context.Context) error)
		if !ok {
			return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "auth.custom.close", "close function has the wrong signature")
		}
		closeFunc = fn
	}

	if cfg.CacheSize == 0 {
		cfg.CacheSize = 100
	}

	if cfg.CacheSize < 0 {
		return &Custom{cfg, nil, nil, validationFunc, closeFunc}, nil
	}

	lock := keymutex.NewHashed(-1)

	cache, err := otter.MustBuilder[string, authentication.ValidationResult](cfg.CacheSize).Build()
	if err != nil {
		return nil, err
	}

	return &Custom{cfg, &cache, lock, validationFunc, closeFunc}, nil
}

func (c Custom) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	slog.Log(ctx, config.LevelTrace, "Performing custom auth check")
	tok, ok := extractToken(ctx, req, c.Token)
	if ok {
		var valResult authentication.ValidationResult
		var inCache bool

		if c.cache != nil {
			c.locks.LockKey(tok)
			defer func() {
				err := c.locks.UnlockKey(tok)
				if err != nil {
					slog.ErrorContext(ctx, "Unable to release auth lock - this token might encounter issues going forward", "error", err)
				}
			}()

			valResult, inCache = c.cache.Get(tok)
			if !inCache {
				slog.Log(ctx, config.LevelTrace, "Cache miss")
			}
		}

		if !inCache {
			valResult = c.validationFunc(tok)
			slog.DebugContext(ctx, fmt.Sprintf("Custom auth check returned %v", valResult.Pass))

			if c.cache != nil {
				c.cache.Set(tok, valResult)
			}
		}

		if checkValidationResult(valResult) {
			uid, _ := pkg.UserIDFromContext(ctx)
			*uid = valResult.UserID

			tid, _ := pkg.TenantIDFromContext(ctx)
			*tid = valResult.TenantID

			if len(valResult.AllowedLayers) > 0 {
				limitLayers, _ := pkg.LimitLayersFromContext(ctx)
				*limitLayers = true

				allowedLayers, _ := pkg.AllowedLayersFromContext(ctx)
				*allowedLayers = valResult.AllowedLayers
			}
			slog.Log(ctx, config.LevelTrace, "Custom auth passed", "result", valResult)

			return true
		}
	} else {
		slog.Log(ctx, config.LevelTrace, "Request lacked any auth token")
	}

	return false
}

// Close calls the script's close function when it defines one. The symbol is optional so scripts written
// before this existed keep working
func (c Custom) Close(ctx context.Context) error {
	if c.closeFunc == nil {
		return nil
	}

	return c.closeFunc(ctx)
}
