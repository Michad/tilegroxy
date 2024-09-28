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

package cache

import (
	"context"

	"github.com/Michad/tilegroxy/pkg"
	"go.opentelemetry.io/otel/codes"
)

// A struct that wraps all other caches in order to add in instrumentation, specifically child spans for tracing the requests to caches. This is used even when telemetry is disabled but OTEL handles no-op'ing in that case so performance impact is minimal
type CacheWrapper struct {
	Name  string
	Cache Cache
}

func (w CacheWrapper) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	newCtx, span := pkg.MakeChildSpan(ctx, &t, "Cache", w.Name, "Lookup")
	defer span.End()

	pc, err := w.Cache.Lookup(newCtx, t)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+w.Name)
	}

	return pc, err
}

func (w CacheWrapper) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	newCtx, span := pkg.MakeChildSpan(ctx, &t, "Cache", w.Name, "Save")
	defer span.End()

	err := w.Cache.Save(newCtx, t, img)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+w.Name)
	}

	return err
}
