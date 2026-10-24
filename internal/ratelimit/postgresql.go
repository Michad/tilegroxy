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

package ratelimit

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresqlConfig struct {
	Datastore string // ID of a postgresql datastore
	Table     string // Never created by tilegroxy. See the docs for the DDL
}

// Trusted config is still interpolated into SQL, so only plain identifiers are allowed
var tableRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*(\.[A-Za-z_][A-Za-z0-9_$]*)?$`)

type Postgresql struct {
	pool     *pgxpool.Pool
	getSQL   string
	incSQL   string
	resetSQL string
}

func init() {
	ratelimit.RegisterRateLimiter(PostgresqlRegistration{})
}

type PostgresqlRegistration struct{}

func (PostgresqlRegistration) InitializeConfig() any {
	return PostgresqlConfig{}
}

func (PostgresqlRegistration) Name() string {
	return "postgresql"
}

func (PostgresqlRegistration) Initialize(cfgAny any, deps ratelimit.RateLimiterDeps) (ratelimit.RateLimiter, error) {
	cfg := cfgAny.(PostgresqlConfig)

	if cfg.Datastore == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "ratelimit.postgresql.datastore")
	}

	if cfg.Table == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "ratelimit.postgresql.table")
	}

	if !tableRegex.MatchString(cfg.Table) {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "ratelimit.postgresql.table", cfg.Table)
	}

	var pool *pgxpool.Pool

	var ok bool

	if deps.Datastores != nil {
		var ds datastore.DatastoreWrapper

		if ds, ok = deps.Datastores.Get(cfg.Datastore); ok {
			pool, ok = ds.Native().(*pgxpool.Pool)
		}
	}

	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "ratelimit.postgresql.datastore", cfg.Datastore)
	}

	table := pgx.Identifier(strings.Split(cfg.Table, ".")).Sanitize()

	return &Postgresql{
		pool: pool,
		getSQL: `SELECT t.key, t.window_start, t.count FROM ` + table + ` t
JOIN unnest($1::text[], $2::timestamptz[]) AS q(key, window_start) ON t.key = q.key AND t.window_start = q.window_start
WHERE t.expires > now()`,
		incSQL: `INSERT INTO ` + table + ` AS t (key, window_start, count, expires)
SELECT q.key, q.window_start, 1, now() + q.ttl * interval '1 millisecond'
FROM unnest($1::text[], $2::timestamptz[], $3::bigint[]) AS q(key, window_start, ttl)
ON CONFLICT (key, window_start) DO UPDATE
SET count = CASE WHEN t.expires > now() THEN t.count + 1 ELSE 1 END, expires = GREATEST(t.expires, EXCLUDED.expires)`,
		resetSQL: `DELETE FROM ` + table + ` t
USING unnest($1::text[], $2::timestamptz[]) AS q(key, window_start)
WHERE t.key = q.key AND t.window_start = q.window_start`,
	}, nil
}

type pgCounterKey struct {
	key    string
	window int64
}

func pgKey(c ratelimit.RateLimitCounter) pgCounterKey {
	return pgCounterKey{c.Key, c.Window.UnixMicro()}
}

// Sorted and deduplicated so concurrent upserts lock rows in the same order and never hit a row twice
func pgArrays(counters []ratelimit.RateLimitCounter) ([]string, []time.Time, []int64) {
	sorted := slices.Clone(counters)
	slices.SortFunc(sorted, func(a, b ratelimit.RateLimitCounter) int {
		return cmp.Or(strings.Compare(a.Key, b.Key), a.Window.Compare(b.Window))
	})
	sorted = slices.CompactFunc(sorted, func(a, b ratelimit.RateLimitCounter) bool {
		return pgKey(a) == pgKey(b)
	})

	keys := make([]string, len(sorted))
	windows := make([]time.Time, len(sorted))
	ttls := make([]int64, len(sorted))

	for i, c := range sorted {
		keys[i] = c.Key
		windows[i] = c.Window
		ttls[i] = c.TTL.Milliseconds()
	}

	return keys, windows, ttls
}

func (p *Postgresql) Get(ctx context.Context, counters []ratelimit.RateLimitCounter) ([]int64, error) {
	keys, windows, _ := pgArrays(counters)

	rows, err := p.pool.Query(ctx, p.getSQL, keys, windows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	found := make(map[pgCounterKey]int64, len(keys))

	for rows.Next() {
		var key string
		var window time.Time
		var count int64

		if err := rows.Scan(&key, &window, &count); err != nil {
			return nil, err
		}

		found[pgCounterKey{key, window.UnixMicro()}] = count
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]int64, len(counters))
	for i, c := range counters {
		result[i] = found[pgKey(c)]
	}

	return result, nil
}

func (p *Postgresql) Increment(ctx context.Context, counters []ratelimit.RateLimitCounter) error {
	keys, windows, ttls := pgArrays(counters)

	_, err := p.pool.Exec(ctx, p.incSQL, keys, windows, ttls)

	return err
}

func (p *Postgresql) Reset(ctx context.Context, counters []ratelimit.RateLimitCounter) error {
	keys, windows, _ := pgArrays(counters)

	_, err := p.pool.Exec(ctx, p.resetSQL, keys, windows)

	return err
}

func (p *Postgresql) Now(ctx context.Context) (time.Time, error) {
	var now time.Time

	err := p.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)

	return now, err
}
