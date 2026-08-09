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

package analytics

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	// This is a hack to help with vscode test execution. Put a .env in repo root w/ anything you need for test containers
	if env, err := os.ReadFile("../../.env"); err == nil {
		envs := strings.Split(string(env), "\n")
		for _, e := range envs {
			if es := strings.Split(e, "="); len(es) == 2 {
				fmt.Printf("Loading env...")
				os.Setenv(es[0], es[1])
			}
		}
	}
}

func extractHostAndPort(t *testing.T, endpoint string) (string, int) {
	split := strings.Split(endpoint, ":")
	port, err := strconv.Atoi(split[1])
	require.NoError(t, err)

	return split[0], port
}

// The DDL documented for the postgres analytics module. Kept in sync with postgres.adoc.
const postgresDDL = `CREATE TABLE tilegroxy_analytics (
	time        TIMESTAMPTZ NOT NULL,
	layer       TEXT        NOT NULL,
	z           INTEGER     NOT NULL,
	x           INTEGER     NOT NULL,
	y           INTEGER     NOT NULL,
	user_id     TEXT,
	extra       JSONB
)`

func setupPostgresContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string, int) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "hunter2",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_DB":       "postgres",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	host, port := extractHostAndPort(t, endpoint)

	return container, host, port
}

func Test_Postgres_WritesEvents(t *testing.T) {
	ctx := pkg.BackgroundContext()
	msgs := config.DefaultConfig().Error.Messages

	_, host, port := setupPostgresContainer(ctx, t)

	dsCfg := []map[string]interface{}{
		{
			"name":     "postgresql",
			"id":       "test",
			"database": "postgres",
			"host":     host,
			"port":     port,
			"user":     "postgres",
			"password": "hunter2",
		},
	}

	datastores, err := datastore.ConstructDatastoreRegistry(dsCfg, nil, msgs)
	require.NoError(t, err)

	wrapper, ok := datastores.Get("test")
	require.True(t, ok)
	pool := wrapper.Native().(*pgxpool.Pool)

	var conn *pgxpool.Conn

	for i := range []int{0, 1, 2, 4, 8} {
		time.Sleep(time.Duration(i) * time.Second)
		conn, err = pool.Acquire(ctx)
		if err == nil {
			break
		}
	}

	require.NoError(t, err)
	_, err = conn.Exec(ctx, postgresDDL)
	require.NoError(t, err)
	conn.Release()

	cfg := PostgresConfig{Datastore: "test", Table: "tilegroxy_analytics"}
	// Force the flush to come from Close so the test doesn't race the age trigger.
	cfg.Batch.MaxSize = 1000
	cfg.Batch.MaxAge = 600

	a, err := PostgresRegistration{}.Initialize(cfg, datastores, msgs)
	require.NoError(t, err)

	require.NoError(t, a.Record(ctx, analytics.Event{
		Time:    time.Now(),
		LayerID: "main",
		Z:       4,
		X:       5,
		Y:       6,
		UserID:  "user-1",
		Fields:  map[string]any{"contenttype": "image/png", "bytes": 1234},
	}))
	require.NoError(t, a.Record(ctx, analytics.Event{Time: time.Now(), LayerID: "other", Z: 1, X: 1, Y: 1}))

	closeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.NoError(t, a.(*Postgres).Close(closeCtx))

	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM tilegroxy_analytics").Scan(&count))
	assert.Equal(t, 2, count)

	var layer, userID, extra string
	var z, x, y int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT layer, z, x, y, user_id, extra::text FROM tilegroxy_analytics WHERE layer = 'main'",
	).Scan(&layer, &z, &x, &y, &userID, &extra))

	assert.Equal(t, "main", layer)
	assert.Equal(t, 4, z)
	assert.Equal(t, 5, x)
	assert.Equal(t, 6, y)
	assert.Equal(t, "user-1", userID)
	assert.Contains(t, extra, "image/png")

	// An anonymous event should still land, with empty rather than absent user.
	var anonUser string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_id FROM tilegroxy_analytics WHERE layer = 'other'").Scan(&anonUser))
	assert.Empty(t, anonUser)
}

func Test_Postgres_InvalidConfig(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	// An empty registry: the datastore lookup must fail cleanly rather than panic.
	empty, err := datastore.ConstructDatastoreRegistry(nil, nil, msgs)
	require.NoError(t, err)

	_, err = PostgresRegistration{}.Initialize(PostgresConfig{Table: "t"}, empty, msgs)
	require.Error(t, err, "datastore is required")

	_, err = PostgresRegistration{}.Initialize(PostgresConfig{Datastore: "x"}, empty, msgs)
	require.Error(t, err, "table is required")

	_, err = PostgresRegistration{}.Initialize(PostgresConfig{Datastore: "x", Table: "bad table"}, empty, msgs)
	require.Error(t, err, "table must be a valid identifier")

	_, err = PostgresRegistration{}.Initialize(PostgresConfig{Datastore: "nonexistent", Table: "t"}, empty, msgs)
	require.Error(t, err, "an unknown datastore id should be reported clearly")
}
