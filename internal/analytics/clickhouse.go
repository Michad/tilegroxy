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
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

type ClickhouseConfig struct {
	analytics.CommonConfig `mapstructure:",squash"`
	// The ID of a datastore with a name of "clickhouse"
	Datastore string
	// The table to insert events into. Tilegroxy never creates it, see the docs for the recommended DDL
	Table string
	// Overrides for the default column names, keyed by the logical field name
	Columns map[string]string
}

type Clickhouse struct {
	ClickhouseConfig
	conn    driver.Conn
	columns map[string]string
	// Precomputed INSERT statement since the column set is fixed at startup
	statement string
	batcher   *analytics.Batcher
}

func init() {
	analytics.RegisterAnalytics(ClickhouseRegistration{})
}

type ClickhouseRegistration struct {
}

func (s ClickhouseRegistration) InitializeConfig() any {
	return ClickhouseConfig{}
}

func (s ClickhouseRegistration) Name() string {
	return "clickhouse"
}

// The logical fields written to columns. Extra fields all share a single map column
var clickhouseDefaultColumns = map[string]string{
	ColumnTime:  ColumnTime,
	ColumnLayer: ColumnLayer,
	ColumnZ:     ColumnZ,
	ColumnX:     ColumnX,
	ColumnY:     ColumnY,
	ColumnUser:  ColumnUser,
	ColumnExtra: ColumnExtra,
}

func (s ClickhouseRegistration) Initialize(cfgAny any, datastores *datastore.DatastoreRegistry, errorMessages config.ErrorMessages) (analytics.Analytics, error) {
	cfg := cfgAny.(ClickhouseConfig)

	if cfg.Datastore == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "analytics.clickhouse.datastore")
	}

	if cfg.Table == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "analytics.clickhouse.table")
	}

	if err := validateIdentifier(cfg.Table, "analytics.clickhouse.table", errorMessages); err != nil {
		return nil, err
	}

	columns, err := resolveColumns(clickhouseDefaultColumns, cfg.Columns, "analytics.clickhouse", errorMessages)
	if err != nil {
		return nil, err
	}

	var conn driver.Conn

	ds, ok := datastores.Get(cfg.Datastore)
	if ok {
		conn, ok = ds.Native().(driver.Conn)
	}

	if !ok {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "analytics.clickhouse.datastore", cfg.Datastore)
	}

	batchCfg, err := analytics.ApplyBatchDefaults(cfg.Batch, errorMessages)
	if err != nil {
		return nil, err
	}

	id := cfg.ID
	if id == "" {
		id = s.Name()
	}

	c := &Clickhouse{
		ClickhouseConfig: cfg,
		conn:             conn,
		columns:          columns,
		statement:        clickhouseStatement(cfg.Table, columns),
	}

	batcher, err := analytics.NewBatcher(id, batchCfg, c.flush)
	if err != nil {
		return nil, err
	}

	c.batcher = batcher

	return c, nil
}

// clickhouseStatement builds the INSERT used for every batch. Only operator-supplied identifiers are
// interpolated, the values are appended as bound parameters by the driver
func clickhouseStatement(table string, columns map[string]string) string {
	ordered := clickhouseColumnOrder(columns)

	return fmt.Sprintf("INSERT INTO %v (%v)", table, strings.Join(ordered, ", "))
}

// clickhouseColumnOrder fixes the column order so it matches the order values are appended in
func clickhouseColumnOrder(columns map[string]string) []string {
	keys := []string{ColumnTime, ColumnLayer, ColumnZ, ColumnX, ColumnY, ColumnUser, ColumnExtra}
	ordered := make([]string, 0, len(keys))

	for _, k := range keys {
		ordered = append(ordered, columns[k])
	}

	return ordered
}

func (c *Clickhouse) Record(ctx context.Context, event analytics.Event) error {
	return c.batcher.Add(ctx, event)
}

func (c *Clickhouse) flush(ctx context.Context, events []analytics.Event) error {
	batch, err := c.conn.PrepareBatch(ctx, c.statement)

	if err != nil {
		return err
	}

	for _, e := range events {
		err = batch.Append(
			e.Time,
			e.LayerID,
			uint8(e.Z),  //nolint:gosec // Zoom is bounds checked long before reaching analytics
			uint32(e.X), //nolint:gosec // Coordinates are validated against the zoom level upstream
			uint32(e.Y), //nolint:gosec // Coordinates are validated against the zoom level upstream
			e.UserID,
			stringifyFields(e.Fields),
		)

		if err != nil {
			// Abort instead of sending a partial batch so a single malformed event doesn't leave half
			// the batch committed and half lost
			_ = batch.Abort()
			return err
		}
	}

	return batch.Send()
}

// stringifyFields flattens the resolved extra fields into the Map(String, String) column. Using a map
// column keeps the table schema stable as operators change which fields they collect
func stringifyFields(fields map[string]any) map[string]string {
	out := make(map[string]string, len(fields))

	for k, v := range fields {
		out[k] = fmt.Sprintf("%v", v)
	}

	return out
}

func (c *Clickhouse) Close(ctx context.Context) error {
	// Only the batcher is closed here, the connection belongs to the datastore registry
	return c.batcher.Close(ctx)
}
