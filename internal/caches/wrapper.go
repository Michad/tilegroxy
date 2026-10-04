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

package caches

import (
	"context"

	"github.com/Michad/tilegroxy/internal/tracing"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"go.opentelemetry.io/otel/codes"
)

// Adds tracing spans around every cache. Always used since OTEL no-ops when telemetry is disabled
type CacheWrapper struct {
	Name  string
	Cache cache.Cache
	// The id a nested cache is registered under, falling back to its configured name
	ref         string
	refExplicit bool
}

func (w CacheWrapper) Unwrap() cache.Cache {
	return w.Cache
}

func (w CacheWrapper) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	newCtx, span := tracing.MakeChildSpan(ctx, &t, "Cache", w.Name, "Lookup")
	defer span.End()

	pc, err := w.Cache.Lookup(newCtx, t)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+w.Name)
	}

	return pc, err
}

// Otherwise the wrapper would hide the inner cache's Closer from the shutdown path
func (w CacheWrapper) Close(ctx context.Context) error {
	return lifecycle.CloseIfCloser(ctx, w.Cache)
}

func (w CacheWrapper) Remove(ctx context.Context, t pkg.TileRequest) (bool, error) {
	newCtx, span := tracing.MakeChildSpan(ctx, &t, "Cache", w.Name, "Remove")
	defer span.End()

	removed, err := w.Cache.Remove(newCtx, t)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+w.Name)
	}

	return removed, err
}

func (w CacheWrapper) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	newCtx, span := tracing.MakeChildSpan(ctx, &t, "Cache", w.Name, "Save")
	defer span.End()

	err := w.Cache.Save(newCtx, t, img)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+w.Name)
	}

	return err
}
