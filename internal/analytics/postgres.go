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

package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresConfig struct {
	analytics.CommonConfig `mapstructure:",squash"`
	// The ID of a datastore with a name of "postgresql"
	Datastore string
	// The table to insert events into. Tilegroxy never creates it, see the docs for the recommended DDL
	Table string
	// Overrides for the default column names, keyed by the logical field name
	Columns map[string]string
}

type Postgres struct {
	PostgresConfig
	pool    *pgxpool.Pool
	columns map[string]string
	// The identifier used by CopyFrom, split so a schema-qualified table works
	tableIdent pgx.Identifier
	batcher    *analytics.Batcher
}

func init() {
	analytics.RegisterAnalytics(PostgresRegistration{})
}

type PostgresRegistration struct {
}

func (s PostgresRegistration) InitializeConfig() any {
	return PostgresConfig{}
}

func (s PostgresRegistration) Name() string {
	return "postgres"
}

var postgresDefaultColumns = map[string]string{
	ColumnTime:  ColumnTime,
	ColumnLayer: ColumnLayer,
	ColumnZ:     ColumnZ,
	ColumnX:     ColumnX,
	ColumnY:     ColumnY,
	ColumnUser:  ColumnUser,
	ColumnExtra: ColumnExtra,
}

func (s PostgresRegistration) Initialize(cfgAny any, datastores *datastore.DatastoreRegistry, errorMessages config.ErrorMessages) (analytics.Analytics, error) {
	cfg := cfgAny.(PostgresConfig)

	if cfg.Datastore == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "analytics.postgres.datastore")
	}

	if cfg.Table == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "analytics.postgres.table")
	}

	if err := validateIdentifier(cfg.Table, "analytics.postgres.table", errorMessages); err != nil {
		return nil, err
	}

	columns, err := resolveColumns(postgresDefaultColumns, cfg.Columns, "analytics.postgres", errorMessages)
	if err != nil {
		return nil, err
	}

	var pool *pgxpool.Pool

	ds, ok := datastores.Get(cfg.Datastore)
	if ok {
		pool, ok = ds.Native().(*pgxpool.Pool)
	}

	if !ok {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "analytics.postgres.datastore", cfg.Datastore)
	}

	batchCfg, err := analytics.ApplyBatchDefaults(cfg.Batch, errorMessages)
	if err != nil {
		return nil, err
	}

	id := cfg.ID
	if id == "" {
		id = s.Name()
	}

	p := &Postgres{
		PostgresConfig: cfg,
		pool:           pool,
		columns:        columns,
		tableIdent:     pgx.Identifier(strings.Split(cfg.Table, ".")),
	}

	batcher, err := analytics.NewBatcher(id, batchCfg, p.flush)
	if err != nil {
		return nil, err
	}

	p.batcher = batcher

	return p, nil
}

func (p *Postgres) Record(ctx context.Context, event analytics.Event) error {
	return p.batcher.Add(ctx, event)
}

func (p *Postgres) flush(ctx context.Context, events []analytics.Event) error {
	columnOrder := []string{ColumnTime, ColumnLayer, ColumnZ, ColumnX, ColumnY, ColumnUser, ColumnExtra}
	columnNames := make([]string, 0, len(columnOrder))

	for _, k := range columnOrder {
		columnNames = append(columnNames, p.columns[k])
	}

	rows := make([][]any, 0, len(events))

	for _, e := range events {
		extra, err := marshalFields(e.Fields)
		if err != nil {
			return err
		}

		rows = append(rows, []any{e.Time, e.LayerID, e.Z, e.X, e.Y, e.UserID, extra})
	}

	// CopyFrom uses the binary COPY protocol which is substantially faster than a multi-row INSERT
	_, err := p.pool.CopyFrom(ctx, p.tableIdent, columnNames, pgx.CopyFromRows(rows))

	return err
}

// marshalFields renders the extra fields as JSON for the jsonb column. A nil map becomes an empty JSON
// object instead of SQL NULL so queries don't have to special-case it
func marshalFields(fields map[string]any) ([]byte, error) {
	if fields == nil {
		return []byte("{}"), nil
	}

	return json.Marshal(fields)
}

func (p *Postgres) Close(ctx context.Context) error {
	// The pool belongs to the datastore registry which closes it separately
	return p.batcher.Close(ctx)
}
