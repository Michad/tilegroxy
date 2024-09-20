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
	ID       string
	Host     string
	Port     uint16
	User     string
	Password string
	Database string
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

func (s PostgresqlWrapperRegistration) InitializeConfig() any {
	cfg := PostgresqlWrapperConfig{}

	cfg.Host = "127.0.0.1"
	cfg.Port = 5432
	cfg.User = "postgresql"
	cfg.Password = ""
	cfg.Database = ""

	return cfg
}

func (s PostgresqlWrapperRegistration) Name() string {
	return "postgresql"
}

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
	spanQueryKey    key = "PostgresqlQuerySpanKey"
	spanBatchKey    key = "PostgresqlBatchSpanKey"
	spanCopyFromKey key = "PostgresqlCopyFromSpanKey"
	spanPrepareKey  key = "PostgresqlPrepareSpanKey"
	spanConnectKey  key = "PostgresqlConnectSpanKey"
	spanAcquireKey  key = "PostgresqlAcquireSpanKey"
)

type Tracer struct {
}

func (t Tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := pkg.MakeChildSpan(ctx, nil, "Postgresql", "Query", "Query")
	ctx = context.WithValue(ctx, spanQueryKey, span)

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("query", data.SQL),
		)
	}

	return ctx
}

func (t Tracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(spanQueryKey).(trace.Span)

	if ok && span != nil && span.IsRecording() {
		if data.Err != nil {
			span.RecordError(data.Err)
			span.SetStatus(codes.Error, data.CommandTag.String())
		}

		span.End()
	}
}

func (t Tracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	ctx, span := pkg.MakeChildSpan(ctx, nil, "Postgresql", "Batch", "Batch")
	ctx = context.WithValue(ctx, spanBatchKey, span)

	if span.IsRecording() {
		if data.Batch != nil {
			span.SetAttributes(
				attribute.Int("length", data.Batch.Len()),
			)
		}
	}

	return ctx
}

func (t Tracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	span, ok := ctx.Value(spanBatchKey).(trace.Span)

	if ok && span != nil && span.IsRecording() {
		span.AddEvent("BatchQuery", trace.WithAttributes(
			attribute.String("query", data.SQL),
		))
	}
}

func (t Tracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	span, ok := ctx.Value(spanBatchKey).(trace.Span)

	if ok && span != nil && span.IsRecording() {
		if data.Err != nil {
			span.RecordError(data.Err)
			span.SetStatus(codes.Error, "")
		}

		span.End()
	}
}

func (t Tracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	ctx, span := pkg.MakeChildSpan(ctx, nil, "Postgresql", "CopyFrom", "CopyFrom")
	ctx = context.WithValue(ctx, spanCopyFromKey, span)

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("table", data.TableName.Sanitize()),
		)
	}

	return ctx
}

func (t Tracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	span, ok := ctx.Value(spanCopyFromKey).(trace.Span)

	if ok && span != nil && span.IsRecording() {
		if data.Err != nil {
			span.RecordError(data.Err)
			span.SetStatus(codes.Error, data.CommandTag.String())
		}

		span.End()
	}
}

func (t Tracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	ctx, span := pkg.MakeChildSpan(ctx, nil, "Postgresql", "Prepare", "Prepare")
	ctx = context.WithValue(ctx, spanPrepareKey, span)

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("query", data.SQL),
		)
	}

	return ctx
}

func (t Tracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	span, ok := ctx.Value(spanPrepareKey).(trace.Span)

	if ok && span != nil && span.IsRecording() {
		if data.Err != nil {
			span.RecordError(data.Err)
			span.SetStatus(codes.Error, "")
		}

		span.End()
	}
}

func (t Tracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	ctx, span := pkg.MakeChildSpan(ctx, nil, "Postgresql", "Connect", "Connect")
	ctx = context.WithValue(ctx, spanConnectKey, span)

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("host", data.ConnConfig.Host),
		)
	}

	return ctx
}

func (t Tracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	span, ok := ctx.Value(spanConnectKey).(trace.Span)

	if ok && span != nil && span.IsRecording() {
		if data.Err != nil {
			span.RecordError(data.Err)
			span.SetStatus(codes.Error, "")
		}

		span.End()
	}
}

func (t Tracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireStartData) context.Context {
	ctx, span := pkg.MakeChildSpan(ctx, nil, "Postgresql", "Acquire", "Acquire")
	ctx = context.WithValue(ctx, spanAcquireKey, span)

	return ctx
}

func (t Tracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	span, ok := ctx.Value(spanAcquireKey).(trace.Span)

	if ok && span != nil && span.IsRecording() {
		if data.Err != nil {
			span.RecordError(data.Err)
			span.SetStatus(codes.Error, "")
		}

		span.End()
	}
}
