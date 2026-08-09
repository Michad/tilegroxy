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
	"log/slog"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"go.opentelemetry.io/otel/codes"
)

// AnalyticsWrapper adds tracing to an analytics module. Unlike the cache and provider wrappers it also absorbs
// errors; a broken analytics destination must never degrade tile serving
type AnalyticsWrapper struct {
	Name      string
	ID        string // Identifies this destination in logs. Defaults to the module name
	Analytics Analytics
	// Built from the same raw config the module sees so field validation happens once at startup
	resolver *fieldResolver
}

// Empty reports whether analytics is effectively unconfigured, letting the handler skip building an Event
// at all. True for the nil wrapper and for the noop module
func (w *AnalyticsWrapper) Empty() bool {
	return w == nil || w.Name == "" || w.Name == noneName
}

// RecordEvent resolves the configured fields for the event and hands it to the module. Separate from Record
// so the caller doesn't need to know how fields are sourced
func (w *AnalyticsWrapper) RecordEvent(ctx context.Context, event Event, src FieldSource) {
	if w == nil {
		return
	}

	event.Fields = w.resolver.Resolve(ctx, src)

	// Errors are already logged and absorbed by Record
	_ = w.Record(ctx, event)
}

func (w *AnalyticsWrapper) Record(ctx context.Context, event Event) error {
	newCtx, span := pkg.MakeChildSpan(ctx, nil, "Analytics", w.Name, "Record")
	defer span.End()

	err := w.Analytics.Record(newCtx, event)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+w.Name)
		slog.WarnContext(newCtx, "Analytics module "+w.ID+" failed to record an event: "+err.Error())
	}

	return nil
}

// Close forwards to the wrapped module so batched events get flushed on shutdown and hot reload
func (w *AnalyticsWrapper) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}

	return lifecycle.CloseIfCloser(ctx, w.Analytics)
}
