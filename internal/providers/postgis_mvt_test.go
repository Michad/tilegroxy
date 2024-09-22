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

//go:build !unit

package providers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"

	_ "github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
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

func setupPostgisContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
	t.Log("setup container")

	req := testcontainers.ContainerRequest{
		Image:        "postgis/postgis:latest",
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor:   wait.ForExposedPort(),
		Env:          map[string]string{"POSTGRES_PASSWORD": "hunter2"},
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	return container, func(t *testing.T) {
		t.Log("teardown container")

		err := container.Terminate(ctx)
		require.NoError(t, err)
	}
}

func Test_Validate(t *testing.T) {

	reg := PostgisMvtRegistration{}
	cfg := reg.InitializeConfig().(PostgisMvtConfig)
	cfg.Datastore = "test"
	cfg.Table = "test"
	cfg.Geometry = "GEOM"
	cfg.Attributes = []string{"str"}
	cfg.Filter = "1=1"
	cfg.Limit = 1

	cfg.GID = "; safkjas @(%$)!@IU%"
	_, err := reg.Initialize(cfg, config.DefaultConfig().Client, config.DefaultConfig().Error.Messages, nil, &datastore.DatastoreRegistry{})
	require.Error(t, err)
	cfg.GID = "gid"

	cfg.Geometry = "; safkjas @(%$)!@IU%"
	_, err = reg.Initialize(cfg, config.DefaultConfig().Client, config.DefaultConfig().Error.Messages, nil, &datastore.DatastoreRegistry{})
	require.Error(t, err)
	cfg.Geometry = "GEOM"

	cfg.Attributes = []string{"; safkjas @(%$)!@IU%"}
	_, err = reg.Initialize(cfg, config.DefaultConfig().Client, config.DefaultConfig().Error.Messages, nil, &datastore.DatastoreRegistry{})
	require.Error(t, err)
	cfg.Attributes = []string{"str"}

	_, err = reg.Initialize(cfg, config.DefaultConfig().Client, config.DefaultConfig().Error.Messages, nil, &datastore.DatastoreRegistry{})
	require.Error(t, err)

}

func Test_GenerateTile(t *testing.T) {
	slog.SetLogLoggerLevel(config.LevelAbsurd)

	ctx := context.Background()
	container, cleanupF := setupPostgisContainer(ctx, t)
	if !assert.NotNil(t, container) {
		return
	}

	defer cleanupF(t)

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	host, port := extractHostAndPort(t, endpoint)

	dsCfg := []map[string]interface{}{
		{
			"name":     "postgresql",
			"id":       "test",
			"host":     host,
			"port":     port,
			"username": "postgres",
			"password": "hunter2",
		},
	}

	datastore, err := datastore.ConstructDatastoreRegistry(dsCfg, nil, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	wrapper, ok := datastore.Get("test")
	require.True(t, ok)
	pg := wrapper.Native().(*pgxpool.Pool)
	conn, err := pg.Acquire(ctx)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "CREATE TABLE test AS SELECT ST_SetSRID(ST_MakePoint(10, 10), 4326) AS \"GEOM\", 0 AS gid, 'hello' AS str")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "totally invalid query")
	require.Error(t, err)
	conn.Release()

	reg := PostgisMvtRegistration{}
	cfg := reg.InitializeConfig().(PostgisMvtConfig)
	cfg.Datastore = "test"
	cfg.Table = "test"
	cfg.GID = "gid"
	cfg.Geometry = "GEOM"
	cfg.Attributes = []string{"str"}
	cfg.Filter = "{z}=0 AND {layer.test}::text IS NULL"
	cfg.Limit = 1

	prov, err := reg.Initialize(cfg, config.DefaultConfig().Client, config.DefaultConfig().Error.Messages, nil, datastore)
	require.NoError(t, err)

	provCtx, err := prov.PreAuth(ctx, layer.ProviderContext{})
	require.NoError(t, err)

	img, err := prov.GenerateTile(ctx, provCtx, pkg.TileRequest{LayerName: "test", X: 0, Y: 0, Z: 0})
	require.NoError(t, err)
	assert.NotEmpty(t, img.Content)

	img, err = prov.GenerateTile(ctx, provCtx, pkg.TileRequest{LayerName: "test", X: 0, Y: 0, Z: 1})
	require.NoError(t, err)
	assert.Empty(t, img.Content)
}
