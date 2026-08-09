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
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	_ "github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// The DDL documented for the clickhouse analytics module. Kept in sync with clickhouse.adoc.
const clickhouseDDL = `CREATE TABLE tile_events (
	time      DateTime,
	layer     LowCardinality(String),
	z         UInt8,
	x         UInt32,
	y         UInt32,
	user_id   String,
	extra     Map(String, String)
) ENGINE = MergeTree() ORDER BY (layer, time)`

func setupClickhouseContainer(ctx context.Context, t *testing.T) (string, int) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "clickhouse/clickhouse-server:24-alpine",
		ExposedPorts: []string{"9000/tcp", "8123/tcp"},
		// The server logs to files rather than stdout, so wait on the HTTP port answering /ping
		// instead of scanning container output.
		WaitingFor: wait.ForHTTP("/ping").WithPort("8123/tcp").WithStartupTimeout(3 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	// Ask for the native port specifically: the HTTP port is also exposed for the wait strategy,
	// and an unqualified Endpoint would return whichever comes first.
	host, err := container.Host(ctx)
	require.NoError(t, err)

	mapped, err := container.MappedPort(ctx, "9000/tcp")
	require.NoError(t, err)

	return host, int(mapped.Num())
}

func Test_Clickhouse_WritesEvents(t *testing.T) {
	ctx := pkg.BackgroundContext()
	msgs := config.DefaultConfig().Error.Messages

	host, port := setupClickhouseContainer(ctx, t)

	dsCfg := []map[string]interface{}{
		{
			"name":     "clickhouse",
			"id":       "test",
			"host":     host,
			"port":     port,
			"database": "default",
			"user":     "default",
		},
	}

	datastores, err := datastore.ConstructDatastoreRegistry(dsCfg, nil, msgs)
	require.NoError(t, err)

	wrapper, ok := datastores.Get("test")
	require.True(t, ok)
	conn := wrapper.Native().(driver.Conn)

	for i := range []int{0, 1, 2, 4, 8} {
		time.Sleep(time.Duration(i) * time.Second)
		if err = conn.Ping(ctx); err == nil {
			break
		}
	}

	require.NoError(t, err)
	require.NoError(t, conn.Exec(ctx, clickhouseDDL))

	cfg := ClickhouseConfig{Datastore: "test", Table: "tile_events"}
	// Force the flush to come from Close so the test doesn't race the age trigger.
	cfg.Batch.MaxSize = 1000
	cfg.Batch.MaxAge = 600

	a, err := ClickhouseRegistration{}.Initialize(cfg, datastores, msgs)
	require.NoError(t, err)

	require.NoError(t, a.Record(ctx, analytics.Event{
		Time:    time.Now(),
		LayerID: "main",
		Z:       4,
		X:       5,
		Y:       6,
		UserID:  "user-1",
		Fields:  map[string]any{"contenttype": "image/png"},
	}))
	require.NoError(t, a.Record(ctx, analytics.Event{Time: time.Now(), LayerID: "other", Z: 1, X: 1, Y: 1}))

	closeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.NoError(t, a.(*Clickhouse).Close(closeCtx))

	var count uint64
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM tile_events").Scan(&count))
	assert.EqualValues(t, 2, count)

	var layer, userID string
	var z uint8
	var x, y uint32
	var extra map[string]string

	require.NoError(t, conn.QueryRow(ctx,
		"SELECT layer, z, x, y, user_id, extra FROM tile_events WHERE layer = 'main'",
	).Scan(&layer, &z, &x, &y, &userID, &extra))

	assert.Equal(t, "main", layer)
	assert.EqualValues(t, 4, z)
	assert.EqualValues(t, 5, x)
	assert.EqualValues(t, 6, y)
	assert.Equal(t, "user-1", userID)
	assert.Equal(t, "image/png", extra["contenttype"])
}

func Test_Clickhouse_InvalidConfig(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	empty, err := datastore.ConstructDatastoreRegistry(nil, nil, msgs)
	require.NoError(t, err)

	_, err = ClickhouseRegistration{}.Initialize(ClickhouseConfig{Table: "t"}, empty, msgs)
	require.Error(t, err, "datastore is required")

	_, err = ClickhouseRegistration{}.Initialize(ClickhouseConfig{Datastore: "x"}, empty, msgs)
	require.Error(t, err, "table is required")

	_, err = ClickhouseRegistration{}.Initialize(ClickhouseConfig{Datastore: "x", Table: "events;DROP TABLE x"}, empty, msgs)
	require.Error(t, err, "table must be a valid identifier")

	_, err = ClickhouseRegistration{}.Initialize(ClickhouseConfig{Datastore: "nonexistent", Table: "t"}, empty, msgs)
	require.Error(t, err, "an unknown datastore id should be reported clearly")
}

func Test_Clickhouse_WrongDatastoreType(t *testing.T) {
	ctx := pkg.BackgroundContext()
	msgs := config.DefaultConfig().Error.Messages

	// Pointing the clickhouse module at a postgresql datastore is an easy config mistake; it must
	// produce a clear error rather than a panic from the type assertion.
	dsCfg := []map[string]interface{}{
		{"name": "postgresql", "id": "pg", "host": "127.0.0.1", "port": 5432},
	}

	datastores, err := datastore.ConstructDatastoreRegistry(dsCfg, nil, msgs)
	require.NoError(t, err)

	defer datastores.Close(ctx) //nolint:errcheck // Test cleanup

	_, err = ClickhouseRegistration{}.Initialize(ClickhouseConfig{Datastore: "pg", Table: "t"}, datastores, msgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pg")
}
