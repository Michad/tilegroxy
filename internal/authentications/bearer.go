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
	"encoding/base64"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maypok86/otter"
	"github.com/redis/go-redis/v9"
	"k8s.io/utils/keymutex"
)

const (
	LookupEncodingNone          = "none"
	LookupEncodingBase64        = "base64"
	LookupEncodingBase64URLSafe = "base64urlsafe"
	LookupEncodingBase36        = "base36"
)

var AllLookupEncodings = []string{LookupEncodingNone, LookupEncodingBase64, LookupEncodingBase64URLSafe, LookupEncodingBase36}

const (
	bearerDefaultCacheTTL    = 600
	bearerBase36             = 36
	bearerDefaultUserField   = "user"
	bearerDefaultTenantField = "tenant"

	bearerColumnValid    = "valid"
	bearerColumnUserID   = "user_id"
	bearerColumnTenantID = "tenant_id"
)

type BearerConfig struct {
	Datastore     string                  // ID of a postgresql or redis datastore that holds the tokens. Required
	HeaderName    string                  // The header containing the token. If this is "Authorization" the "Bearer " prefix is required and removed. Defaults to "Authorization"
	TokenEncoding string                  // Encoding applied to the token before the lookup. One of AllLookupEncodings. Defaults to none
	CacheSize     int                     // How many lookup results to keep in memory. Zero or negative disables the cache, which is the default
	CacheTTL      uint32                  // Seconds a lookup result is reused, for both passing and failing tokens. Defaults to 600
	Postgresql    *BearerPostgresqlConfig // Required when Datastore is a postgresql datastore
	Redis         *BearerRedisConfig      // Only allowed when Datastore is a redis datastore
}

type BearerPostgresqlConfig struct {
	Query string // The token is bound to $1. Reads the valid, user_id, and tenant_id columns
}

type BearerRedisConfig struct {
	KeyPrefix   string // Prepended to the token to form the key
	UserField   string // The hash field holding the user ID. Defaults to "user"
	TenantField string // The hash field holding the tenant ID. Defaults to "tenant"
}

type bearerLookupResult struct {
	pass     bool
	userID   string
	tenantID string
}

type bearerLookup func(ctx context.Context, token string) (bearerLookupResult, error)

type Bearer struct {
	BearerConfig
	lookup bearerLookup
	cache  *otter.Cache[string, bearerLookupResult]
	// Only used with the cache, so a page of tiles sharing a token does one lookup
	locks keymutex.KeyMutex
}

func init() {
	authentication.RegisterAuthentication(BearerRegistration{})
}

type BearerRegistration struct {
}

func (s BearerRegistration) InitializeConfig() any {
	return BearerConfig{}
}

func (s BearerRegistration) Name() string {
	return "bearer"
}

func (s BearerRegistration) Initialize(cfgAny any, deps authentication.AuthenticationDeps) (authentication.Authentication, error) {
	cfg := cfgAny.(BearerConfig)

	if cfg.Datastore == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "authentication.datastore")
	}

	if cfg.HeaderName == "" {
		cfg.HeaderName = "Authorization"
	}

	if cfg.TokenEncoding == "" {
		cfg.TokenEncoding = LookupEncodingNone
	}
	if !slices.Contains(AllLookupEncodings, cfg.TokenEncoding) {
		return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "authentication.tokenencoding", cfg.TokenEncoding, AllLookupEncodings)
	}

	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = bearerDefaultCacheTTL
	}

	lookup, err := newBearerLookup(&cfg, deps)
	if err != nil {
		return nil, err
	}

	if cfg.CacheSize <= 0 {
		return &Bearer{BearerConfig: cfg, lookup: lookup}, nil
	}

	cache, err := otter.MustBuilder[string, bearerLookupResult](cfg.CacheSize).
		WithTTL(time.Duration(cfg.CacheTTL) * time.Second).
		Build()
	if err != nil {
		return nil, err
	}

	return &Bearer{BearerConfig: cfg, lookup: lookup, cache: &cache, locks: keymutex.NewHashed(-1)}, nil
}

func newBearerLookup(cfg *BearerConfig, deps authentication.AuthenticationDeps) (bearerLookup, error) {
	if deps.Datastores == nil {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "authentication.datastore", cfg.Datastore)
	}

	ds, ok := deps.Datastores.Get(cfg.Datastore)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "authentication.datastore", cfg.Datastore)
	}

	switch native := ds.Native().(type) {
	case *pgxpool.Pool:
		return newBearerPostgresqlLookup(cfg, native, deps.ErrorMessages)
	case redis.UniversalClient:
		return newBearerRedisLookup(cfg, native, deps.ErrorMessages)
	default:
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "authentication.datastore", cfg.Datastore)
	}
}

func newBearerPostgresqlLookup(cfg *BearerConfig, pool *pgxpool.Pool, errorMessages config.ErrorMessages) (bearerLookup, error) {
	if cfg.Redis != nil {
		return nil, fmt.Errorf(errorMessages.ParamRequiresParam, "authentication.redis", "a redis datastore")
	}

	if cfg.Postgresql == nil || cfg.Postgresql.Query == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "authentication.postgresql.query")
	}

	query := cfg.Postgresql.Query

	// Without $1 the token can't be bound, and interpolating it would allow SQL injection
	if !strings.Contains(query, "$1") {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.postgresql.query", query)
	}

	return func(ctx context.Context, token string) (bearerLookupResult, error) {
		rows, err := pool.Query(ctx, query, token)
		if err != nil {
			return bearerLookupResult{}, err
		}
		defer rows.Close()

		if !rows.Next() {
			return bearerLookupResult{}, rows.Err()
		}

		values, err := rows.Values()
		if err != nil {
			return bearerLookupResult{}, err
		}

		fields := rows.FieldDescriptions()
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Name
		}

		if rows.Next() {
			slog.WarnContext(ctx, "Bearer token query returned more than one row")
			return bearerLookupResult{}, nil
		}
		if err = rows.Err(); err != nil {
			return bearerLookupResult{}, err
		}

		return bearerResultFromColumns(ctx, names, values), nil
	}, nil
}

func bearerResultFromColumns(ctx context.Context, names []string, values []any) bearerLookupResult {
	result := bearerLookupResult{pass: true}

	for i, name := range names {
		val := values[i]

		switch name {
		case bearerColumnValid:
			valid, ok := val.(bool)
			if !ok && val != nil {
				slog.WarnContext(ctx, "Bearer token query returned a non-boolean column", "column", name, "type", fmt.Sprintf("%T", val))
				return bearerLookupResult{}
			}
			if !valid {
				return bearerLookupResult{}
			}
		case bearerColumnUserID:
			if !bearerStringColumn(ctx, name, val, &result.userID) {
				return bearerLookupResult{}
			}
		case bearerColumnTenantID:
			if !bearerStringColumn(ctx, name, val, &result.tenantID) {
				return bearerLookupResult{}
			}
		}
	}

	return result
}

func bearerStringColumn(ctx context.Context, name string, val any, dst *string) bool {
	if val == nil {
		return true
	}

	s, ok := val.(string)
	if !ok {
		slog.WarnContext(ctx, "Bearer token query returned a non-string column", "column", name, "type", fmt.Sprintf("%T", val))
		return false
	}

	*dst = s
	return true
}

func newBearerRedisLookup(cfg *BearerConfig, client redis.UniversalClient, errorMessages config.ErrorMessages) (bearerLookup, error) {
	if cfg.Postgresql != nil {
		return nil, fmt.Errorf(errorMessages.ParamRequiresParam, "authentication.postgresql", "a postgresql datastore")
	}

	if cfg.Redis == nil {
		cfg.Redis = &BearerRedisConfig{}
	}
	if cfg.Redis.UserField == "" {
		cfg.Redis.UserField = bearerDefaultUserField
	}
	if cfg.Redis.TenantField == "" {
		cfg.Redis.TenantField = bearerDefaultTenantField
	}

	prefix, userField, tenantField := cfg.Redis.KeyPrefix, cfg.Redis.UserField, cfg.Redis.TenantField

	return func(ctx context.Context, token string) (bearerLookupResult, error) {
		key := prefix + token

		// HMGET errors on non-hash keys, but TYPE alone decides whether that matters
		pipe := client.Pipeline()
		keyType := pipe.Type(ctx, key)
		fields := pipe.HMGet(ctx, key, userField, tenantField)
		_, _ = pipe.Exec(ctx)

		if err := keyType.Err(); err != nil {
			return bearerLookupResult{}, err
		}

		switch keyType.Val() {
		case "none":
			return bearerLookupResult{}, nil
		case "hash":
			vals, err := fields.Result()
			if err != nil {
				return bearerLookupResult{}, err
			}

			userID, _ := vals[0].(string)
			tenantID, _ := vals[1].(string)
			return bearerLookupResult{pass: true, userID: userID, tenantID: tenantID}, nil
		default:
			return bearerLookupResult{pass: true}, nil
		}
	}, nil
}

func (b Bearer) extractToken(req *http.Request) (string, bool) {
	header := req.Header[b.HeaderName]
	if len(header) != 1 {
		return "", false
	}

	token := header[0]
	if b.HeaderName == "Authorization" {
		var ok bool
		token, ok = strings.CutPrefix(token, "Bearer ")
		if !ok {
			return "", false
		}
	}

	if token == "" {
		return "", false
	}

	return encodeBearerToken(token, b.TokenEncoding), true
}

func encodeBearerToken(token string, encoding string) string {
	switch encoding {
	case LookupEncodingBase64:
		return base64.StdEncoding.EncodeToString([]byte(token))
	case LookupEncodingBase64URLSafe:
		return base64.RawURLEncoding.EncodeToString([]byte(token))
	case LookupEncodingBase36:
		return new(big.Int).SetBytes([]byte(token)).Text(bearerBase36)
	default:
		return token
	}
}

func (b Bearer) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	token, ok := b.extractToken(req)
	if !ok {
		slog.Log(ctx, config.LevelTrace, "Request lacked a bearer token")
		return false
	}

	result, ok := b.cachedLookup(ctx, token)
	if !ok || !result.pass {
		return false
	}

	if ctxUserID, ok := pkg.UserIDFromContext(ctx); ok {
		*ctxUserID = result.userID
	}
	if ctxTenantID, ok := pkg.TenantIDFromContext(ctx); ok {
		*ctxTenantID = result.tenantID
	}

	return true
}

// A TTL cache runs a cleanup goroutine that would otherwise leak on every reload
func (b Bearer) Close(_ context.Context) error {
	if b.cache != nil {
		b.cache.Close()
	}

	return nil
}

// Errors fail closed and aren't cached, so an outage doesn't lock out valid tokens once it recovers
func (b Bearer) cachedLookup(ctx context.Context, token string) (bearerLookupResult, bool) {
	if b.cache != nil {
		b.locks.LockKey(token)
		defer func() {
			if err := b.locks.UnlockKey(token); err != nil {
				slog.ErrorContext(ctx, "Unable to release auth lock - this token might encounter issues going forward", "error", err)
			}
		}()

		if result, ok := b.cache.Get(token); ok {
			return result, true
		}
	}

	result, err := b.lookup(ctx, token)
	if err != nil {
		slog.ErrorContext(ctx, "Bearer token lookup failed", "datastore", b.Datastore, "error", err)
		return bearerLookupResult{}, false
	}

	if b.cache != nil {
		b.cache.Set(token, result)
	}

	return result, true
}
