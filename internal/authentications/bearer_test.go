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
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDatastore struct {
	id     string
	native any
}

func (f fakeDatastore) GetID() string { return f.id }
func (f fakeDatastore) Native() any   { return f.native }

type fakeDatastoreRegistry map[string]datastore.DatastoreWrapper

func (f fakeDatastoreRegistry) Get(id string) (datastore.DatastoreWrapper, bool) {
	ds, ok := f[id]
	return ds, ok
}

func bearerTestDeps(t *testing.T) authentication.AuthenticationDeps {
	t.Helper()

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })

	return authentication.AuthenticationDeps{
		ErrorMessages: config.DefaultConfig().Error.Messages,
		Datastores: fakeDatastoreRegistry{
			"pg":    fakeDatastore{"pg", (*pgxpool.Pool)(nil)},
			"redis": fakeDatastore{"redis", client},
			"other": fakeDatastore{"other", "not a client"},
		},
	}
}

func bearerRequest(t *testing.T, headers map[string]string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)
	require.NoError(t, err)

	for k, v := range headers {
		req.Header[k] = []string{v}
	}

	return req
}

func Test_Bearer_InitializeValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]interface{}
		err  string
	}{
		{"missing datastore", map[string]interface{}{"postgresql": map[string]interface{}{"query": "SELECT 1 WHERE $1 = 'a'"}}, "authentication.datastore is required"},
		{"unknown datastore", map[string]interface{}{"datastore": "nope"}, "authentication.datastore: nope"},
		{"unsupported datastore", map[string]interface{}{"datastore": "other"}, "authentication.datastore: other"},
		{"bad encoding", map[string]interface{}{"datastore": "redis", "tokenencoding": "md5"}, "authentication.tokenencoding"},
		{"postgres without block", map[string]interface{}{"datastore": "pg"}, "authentication.postgresql.query is required"},
		{"postgres without query", map[string]interface{}{"datastore": "pg", "postgresql": map[string]interface{}{}}, "authentication.postgresql.query is required"},
		{"postgres query without bind", map[string]interface{}{"datastore": "pg", "postgresql": map[string]interface{}{"query": "SELECT 'u'"}}, "authentication.postgresql.query"},
		{"postgres with redis block", map[string]interface{}{"datastore": "pg", "postgresql": map[string]interface{}{"query": "SELECT $1"}, "redis": map[string]interface{}{"keyprefix": "k:"}}, "authentication.redis can only be set when a redis datastore"},
		{"redis with postgres block", map[string]interface{}{"datastore": "redis", "postgresql": map[string]interface{}{"query": "SELECT $1"}}, "authentication.postgresql can only be set when a postgresql datastore"},
		{"misplaced redis key", map[string]interface{}{"datastore": "redis", "keyprefix": "k:"}, "keyprefix"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg["name"] = "bearer"
			_, err := ConstructAuth(tc.cfg, bearerTestDeps(t))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.err)
		})
	}
}

func Test_Bearer_InitializeWithoutDatastoreRegistryErrors(t *testing.T) {
	_, err := BearerRegistration{}.Initialize(BearerConfig{Datastore: "pg"}, authentication.AuthenticationDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.Error(t, err)
}

func Test_Bearer_InitializeDefaults(t *testing.T) {
	auth, err := BearerRegistration{}.Initialize(BearerConfig{Datastore: "redis"}, bearerTestDeps(t))
	require.NoError(t, err)

	b := auth.(*Bearer)
	assert.Equal(t, "Authorization", b.HeaderName)
	assert.Equal(t, LookupEncodingNone, b.TokenEncoding)
	assert.Equal(t, uint32(bearerDefaultCacheTTL), b.CacheTTL)
	assert.Equal(t, bearerDefaultUserField, b.Redis.UserField)
	assert.Equal(t, bearerDefaultTenantField, b.Redis.TenantField)
	assert.Nil(t, b.cache)

	auth, err = BearerRegistration{}.Initialize(BearerConfig{Datastore: "pg", Postgresql: &BearerPostgresqlConfig{Query: "SELECT u FROM t WHERE k = $1"}, CacheSize: 10}, bearerTestDeps(t))
	require.NoError(t, err)
	assert.NotNil(t, auth.(*Bearer).cache)
	require.NoError(t, auth.(*Bearer).Close(context.Background()))
}

func Test_Bearer_ExtractToken(t *testing.T) {
	auth := Bearer{BearerConfig: BearerConfig{HeaderName: "Authorization", TokenEncoding: LookupEncodingNone}}

	tok, ok := auth.extractToken(bearerRequest(t, map[string]string{"Authorization": "Bearer abc"}))
	assert.True(t, ok)
	assert.Equal(t, "abc", tok)

	_, ok = auth.extractToken(bearerRequest(t, map[string]string{"Authorization": "Basic abc"}))
	assert.False(t, ok)

	_, ok = auth.extractToken(bearerRequest(t, map[string]string{"Authorization": "Bearer "}))
	assert.False(t, ok)

	_, ok = auth.extractToken(bearerRequest(t, nil))
	assert.False(t, ok)

	custom := Bearer{BearerConfig: BearerConfig{HeaderName: "X-Api-Key", TokenEncoding: LookupEncodingBase36}}
	tok, ok = custom.extractToken(bearerRequest(t, map[string]string{"X-Api-Key": "abc"}))
	assert.True(t, ok)
	assert.Equal(t, "3ssir", tok)
}

func Test_Bearer_EncodeToken(t *testing.T) {
	assert.Equal(t, "\xfb\xffabc", encodeBearerToken("\xfb\xffabc", LookupEncodingNone))
	assert.Equal(t, "+/9hYmM=", encodeBearerToken("\xfb\xffabc", LookupEncodingBase64))
	assert.Equal(t, "-_9hYmM", encodeBearerToken("\xfb\xffabc", LookupEncodingBase64URLSafe))
	assert.Equal(t, "3ssir", encodeBearerToken("abc", LookupEncodingBase36))
}

func newFakeBearer(t *testing.T, cacheSize int, lookup bearerLookup) *Bearer {
	t.Helper()

	auth, err := BearerRegistration{}.Initialize(BearerConfig{Datastore: "redis", CacheSize: cacheSize}, bearerTestDeps(t))
	require.NoError(t, err)

	b := auth.(*Bearer)
	b.lookup = lookup
	t.Cleanup(func() { _ = b.Close(context.Background()) })
	return b
}

func Test_Bearer_CheckAuthenticationSetsContext(t *testing.T) {
	b := newFakeBearer(t, -1, func(_ context.Context, token string) (bearerLookupResult, error) {
		if token == "good" {
			return bearerLookupResult{pass: true, userID: "alice", tenantID: "acme"}, nil
		}
		return bearerLookupResult{}, nil
	})

	ctx := pkg.BackgroundContext()
	assert.True(t, b.CheckAuthentication(ctx, bearerRequest(t, map[string]string{"Authorization": "Bearer good"})))

	userID, _ := pkg.UserIDFromContext(ctx)
	tenantID, _ := pkg.TenantIDFromContext(ctx)
	assert.Equal(t, "alice", *userID)
	assert.Equal(t, "acme", *tenantID)

	assert.False(t, b.CheckAuthentication(pkg.BackgroundContext(), bearerRequest(t, map[string]string{"Authorization": "Bearer bad"})))
	assert.False(t, b.CheckAuthentication(pkg.BackgroundContext(), bearerRequest(t, nil)))
}

func Test_Bearer_CachesResultsButNotErrors(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)

	b := newFakeBearer(t, 100, func(_ context.Context, token string) (bearerLookupResult, error) {
		calls.Add(1)
		if fail.Load() {
			return bearerLookupResult{}, errors.New("datastore down")
		}
		return bearerLookupResult{pass: token == "good", userID: "alice"}, nil
	})

	good := map[string]string{"Authorization": "Bearer good"}
	bad := map[string]string{"Authorization": "Bearer bad"}

	assert.False(t, b.CheckAuthentication(pkg.BackgroundContext(), bearerRequest(t, good)))
	fail.Store(false)
	assert.True(t, b.CheckAuthentication(pkg.BackgroundContext(), bearerRequest(t, good)))
	assert.Equal(t, int32(2), calls.Load())

	ctx := pkg.BackgroundContext()
	assert.True(t, b.CheckAuthentication(ctx, bearerRequest(t, good)))
	userID, _ := pkg.UserIDFromContext(ctx)
	assert.Equal(t, "alice", *userID)

	assert.False(t, b.CheckAuthentication(pkg.BackgroundContext(), bearerRequest(t, bad)))
	assert.False(t, b.CheckAuthentication(pkg.BackgroundContext(), bearerRequest(t, bad)))
	assert.Equal(t, int32(3), calls.Load())
}

func Test_Bearer_ConcurrentRequestsShareOneLookup(t *testing.T) {
	var calls atomic.Int32
	b := newFakeBearer(t, 100, func(_ context.Context, _ string) (bearerLookupResult, error) {
		calls.Add(1)
		return bearerLookupResult{pass: true}, nil
	})

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.True(t, b.CheckAuthentication(pkg.BackgroundContext(), bearerRequest(t, map[string]string{"Authorization": "Bearer tok"})))
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), calls.Load())
}

func Test_Bearer_Close(t *testing.T) {
	auth, err := ConstructAuth(map[string]interface{}{"name": "bearer", "datastore": "redis", "cachesize": 10}, bearerTestDeps(t))
	require.NoError(t, err)
	assert.NoError(t, lifecycle.CloseIfCloser(context.Background(), auth))

	uncached := newFakeBearer(t, -1, nil)
	assert.NoError(t, uncached.Close(context.Background()))
}

func Test_Bearer_ResultFromColumns(t *testing.T) {
	tests := []struct {
		name   string
		names  []string
		values []any
		want   bearerLookupResult
	}{
		{"no columns", nil, nil, bearerLookupResult{pass: true}},
		{"ids", []string{"user_id", "tenant_id", "other"}, []any{"alice", "acme", int64(1)}, bearerLookupResult{pass: true, userID: "alice", tenantID: "acme"}},
		{"null ids", []string{"user_id", "tenant_id"}, []any{nil, nil}, bearerLookupResult{pass: true}},
		{"valid", []string{"valid", "user_id"}, []any{true, "alice"}, bearerLookupResult{pass: true, userID: "alice"}},
		{"invalid", []string{"valid", "user_id"}, []any{false, "alice"}, bearerLookupResult{}},
		{"null valid", []string{"valid"}, []any{nil}, bearerLookupResult{}},
		{"non-boolean valid", []string{"valid"}, []any{"true"}, bearerLookupResult{}},
		{"non-string user", []string{"user_id"}, []any{int64(42)}, bearerLookupResult{}},
		{"non-string tenant", []string{"tenant_id"}, []any{[16]byte{}}, bearerLookupResult{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, bearerResultFromColumns(context.Background(), tc.names, tc.values))
		})
	}
}
