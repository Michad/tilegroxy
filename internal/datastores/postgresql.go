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

package datastores

import (
	"context"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type PostgresqlWrapperConfig struct {
	ID             string
	Host           string
	Port           uint16
	User           string
	Password       string
	Database       string
	MinConnections int32
	MaxConnections int32
	IdleTimeout    int32 // In seconds
	Lifetime       int32 // In seconds
}

type PostgresqlWrapper struct {
	PostgresqlWrapperConfig
	pool *pgxpool.Pool
}

func init() {
	datastore.RegisterDatastoreWrapper(PostgresqlWrapperRegistration{})
}

type PostgresqlWrapperRegistration struct {
}

//nolint:mnd
func (s PostgresqlWrapperRegistration) InitializeConfig() any {
	cfg := PostgresqlWrapperConfig{}

	cfg.Host = "127.0.0.1"
	cfg.Port = 5432
	cfg.User = "postgres"
	cfg.Password = ""
	cfg.Database = ""
	cfg.MinConnections = 10
	cfg.MaxConnections = 30
	cfg.IdleTimeout = 60 * 10
	cfg.Lifetime = 60 * 60 * 24

	return cfg
}

func (s PostgresqlWrapperRegistration) Name() string {
	return "postgresql"
}

//nolint:mnd
func (s PostgresqlWrapperRegistration) Initialize(cfgAny any, _ secret.Secreter, _ config.ErrorMessages) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(PostgresqlWrapperConfig)

	dbCfg, err := pgxpool.ParseConfig("")

	if err != nil {
		return nil, err
	}

	dbCfg.ConnConfig.Host = cfg.Host
	dbCfg.ConnConfig.Port = cfg.Port
	dbCfg.ConnConfig.Database = cfg.Database
	dbCfg.ConnConfig.User = cfg.User
	dbCfg.ConnConfig.Password = cfg.Password
	dbCfg.ConnConfig.Host = cfg.Host
	dbCfg.ConnConfig.Tracer = Tracer{}
	dbCfg.MinConns = cfg.MinConnections
	dbCfg.MaxConns = cfg.MaxConnections
	dbCfg.MaxConnIdleTime = time.Duration(cfg.IdleTimeout) * time.Second
	dbCfg.MaxConnLifetime = time.Duration(cfg.Lifetime) * time.Second
	dbCfg.MaxConnLifetimeJitter = time.Duration(cfg.Lifetime) / 10 * time.Second

	dbpool, err := pgxpool.NewWithConfig(pkg.BackgroundContext(), dbCfg)

	if err != nil {
		return nil, err
	}

	return &PostgresqlWrapper{cfg, dbpool}, nil
}

func (p PostgresqlWrapper) GetID() string {
	return p.ID
}

func (p PostgresqlWrapper) Native() any {
	return p.pool
}

type key string

const (
	spanQueryKey    key = "Query"
	spanBatchKey    key = "Batch"
	spanCopyFromKey key = "CopyFrom"
	spanPrepareKey  key = "Prepare"
	spanConnectKey  key = "Connect"
	spanAcquireKey  key = "Acquire"
)

type Tracer struct {
}

func (t Tracer) traceStart(ctx context.Context, myKey key, query, host string) context.Context {
	ctx, span := pkg.MakeChildSpan(ctx, nil, "Postgresql", string(myKey), string(myKey))
	ctx = context.WithValue(ctx, myKey, span)

		att := make([]attribute.KeyValue, 0)
		if query != "" {
			att = append(att, attribute.String("query", query))
		}
		if host != "" {
			att = append(att, attribute.String("host", host))
		}

		span.SetAttributes(att...)

	return ctx
}

func (t Tracer) traceEnd(ctx context.Context, myKey key, err error, msg string) {
	span, ok := ctx.Value(myKey).(trace.Span)

	if ok && span != nil {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, msg)
		}

		span.End()
	}
}

func (t Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return t.traceStart(ctx, spanQueryKey, data.SQL, "")
}

func (t Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	t.traceEnd(ctx, spanQueryKey, data.Err, data.CommandTag.String())
}

func (t Tracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	return t.traceStart(ctx, spanPrepareKey, data.SQL, "")
}

func (t Tracer) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareEndData) {
	t.traceEnd(ctx, spanPrepareKey, data.Err, "")
}

func (t Tracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	return t.traceStart(ctx, spanConnectKey, "", data.ConnConfig.Host)
}

func (t Tracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	t.traceEnd(ctx, spanConnectKey, data.Err, "")
}

func (t Tracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	return t.traceStart(ctx, spanAcquireKey, "", "")
}

func (t Tracer) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	t.traceEnd(ctx, spanAcquireKey, data.Err, "")
}
