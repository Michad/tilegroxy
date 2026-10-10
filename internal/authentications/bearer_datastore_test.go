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

//go:build !unit

package authentications

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startBearerContainer(ctx context.Context, t *testing.T, req testcontainers.ContainerRequest) (string, int) {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	host, portStr, _ := strings.Cut(endpoint, ":")
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return host, port
}

func constructBearerWithDatastore(ctx context.Context, t *testing.T, dsCfg map[string]interface{}, authCfg map[string]interface{}) authentication.Authentication {
	t.Helper()

	msgs := config.DefaultConfig().Error.Messages
	reg, err := datastores.ConstructDatastoreRegistry(ctx, []map[string]interface{}{dsCfg}, nil, msgs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reg.Close(context.Background()) })

	auth, err := ConstructAuth(authCfg, authentication.AuthenticationDeps{ErrorMessages: msgs, Datastores: reg})
	require.NoError(t, err)

	return auth
}

func assertBearer(t *testing.T, auth authentication.Authentication, header string, pass bool, userID, tenantID string) {
	t.Helper()

	ctx := pkg.BackgroundContext()
	assert.Equal(t, pass, auth.CheckAuthentication(ctx, bearerRequest(t, map[string]string{"Authorization": header})))

	ctxUserID, _ := pkg.UserIDFromContext(ctx)
	ctxTenantID, _ := pkg.TenantIDFromContext(ctx)
	assert.Equal(t, userID, *ctxUserID)
	assert.Equal(t, tenantID, *ctxTenantID)
}

func Test_Bearer_Postgresql(t *testing.T) {
	ctx := context.Background()
	host, port := startBearerContainer(ctx, t, testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_PASSWORD": "hunter2", "POSTGRES_USER": "postgres", "POSTGRES_DB": "postgres"},
		WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2 * time.Minute),
	})

	dsCfg := map[string]interface{}{"name": "postgresql", "id": "auth_db", "host": host, "port": port, "password": "hunter2", "database": "postgres", "minconnections": 1}

	pool, err := pgxpool.New(ctx, "postgres://postgres:hunter2@"+host+":"+strconv.Itoa(port)+"/postgres")
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, `CREATE TABLE api_keys (token TEXT PRIMARY KEY, user_id TEXT, tenant_id TEXT, uid UUID, revoked BOOLEAN);
		INSERT INTO api_keys VALUES ('good', 'alice', 'acme', '00010203-0405-0607-0809-0a0b0c0d0e0f', false), ('revoked', 'bob', NULL, NULL, true), ('unknown', NULL, NULL, NULL, NULL), (encode('encoded', 'base64'), NULL, 'globex', NULL, false)`)
	require.NoError(t, err)

	auth := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":       "bearer",
		"datastore":  "auth_db",
		"postgresql": map[string]interface{}{"query": "SELECT user_id, tenant_id, NOT revoked AS valid, 1 AS ignored FROM api_keys WHERE token = $1"},
	})

	assertBearer(t, auth, "Bearer good", true, "alice", "acme")
	assertBearer(t, auth, "Bearer revoked", false, "", "")
	assertBearer(t, auth, "Bearer unknown", false, "", "")
	assertBearer(t, auth, "Bearer missing", false, "", "")
	assertBearer(t, auth, "Bearer x' OR '1'='1", false, "", "")

	encoded := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":          "bearer",
		"datastore":     "auth_db",
		"tokenencoding": "base64",
		"postgresql":    map[string]interface{}{"query": "SELECT user_id, tenant_id FROM api_keys WHERE token = $1"},
	})
	assertBearer(t, encoded, "Bearer encoded", true, "", "globex")
	assertBearer(t, encoded, "Bearer good", false, "", "")

	noColumns := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":       "bearer",
		"datastore":  "auth_db",
		"postgresql": map[string]interface{}{"query": "SELECT 1 FROM api_keys WHERE token = $1"},
	})
	assertBearer(t, noColumns, "Bearer revoked", true, "", "")

	wrongType := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":       "bearer",
		"datastore":  "auth_db",
		"postgresql": map[string]interface{}{"query": "SELECT uid AS user_id FROM api_keys WHERE token = $1"},
	})
	assertBearer(t, wrongType, "Bearer good", false, "", "")

	multipleRows := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":       "bearer",
		"datastore":  "auth_db",
		"postgresql": map[string]interface{}{"query": "SELECT user_id FROM api_keys WHERE token = $1 OR token = 'revoked'"},
	})
	assertBearer(t, multipleRows, "Bearer good", false, "", "")

	broken := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":       "bearer",
		"datastore":  "auth_db",
		"postgresql": map[string]interface{}{"query": "SELECT user_id FROM no_such_table WHERE token = $1"},
	})
	assertBearer(t, broken, "Bearer good", false, "", "")
}

func Test_Bearer_Redis(t *testing.T) {
	ctx := context.Background()
	host, port := startBearerContainer(ctx, t, testcontainers.ContainerRequest{
		Image:        "redis:latest",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	})

	client := redis.NewClient(&redis.Options{Addr: host + ":" + strconv.Itoa(port)})
	defer client.Close()

	require.NoError(t, client.HSet(ctx, "apikey:good", "user", "alice", "tenant", "acme", "org", "globex").Err())
	require.NoError(t, client.Set(ctx, "apikey:string", "bob", 0).Err())
	require.NoError(t, client.SAdd(ctx, "apikey:set", "bob").Err())

	dsCfg := map[string]interface{}{"name": "redis", "id": "auth_redis", "host": host, "port": port}

	auth := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":      "bearer",
		"datastore": "auth_redis",
		"redis":     map[string]interface{}{"keyprefix": "apikey:"},
	})

	assertBearer(t, auth, "Bearer good", true, "alice", "acme")
	assertBearer(t, auth, "Bearer missing", false, "", "")
	assertBearer(t, auth, "Bearer string", true, "", "")
	assertBearer(t, auth, "Bearer set", true, "", "")

	cached := constructBearerWithDatastore(ctx, t, dsCfg, map[string]interface{}{
		"name":      "bearer",
		"datastore": "auth_redis",
		"redis":     map[string]interface{}{"keyprefix": "apikey:", "tenantfield": "org"},
		"cachesize": 10,
	})
	assertBearer(t, cached, "Bearer good", true, "alice", "globex")

	require.NoError(t, client.Del(ctx, "apikey:good").Err())
	assertBearer(t, cached, "Bearer good", true, "alice", "globex")
	assertBearer(t, auth, "Bearer good", false, "", "")
}
