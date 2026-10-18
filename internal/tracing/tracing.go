// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tracing

import (
	"context"

	"github.com/Michad/tilegroxy/internal/static"
	"github.com/Michad/tilegroxy/pkg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var packageName = static.GetPackage()

var version, ref, buildDate = static.GetVersionInformation()

var tracer trace.Tracer = otel.Tracer(packageName)

// A new context and span for entity wrappers to break down request flow. Callers must End the returned span
func MakeChildSpan(ctx context.Context, newRequest *pkg.TileRequest, providerName string, childSpanName string, functionName string) (context.Context, trace.Span) {
	spanName := providerName

	if childSpanName != "" {
		spanName += "-" + childSpanName
	}

	newCtx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("service.name", "tilegroxy"),
			attribute.String("service.version", version+"-"+ref),
			attribute.String("service.build", buildDate),
			attribute.String("code.function", functionName),
		)

		if newRequest != nil {
			span.SetAttributes(
				attribute.String("tilegroxy.layer.name", newRequest.LayerName),
				attribute.Int("tilegroxy.coordinate.x", newRequest.X),
				attribute.Int("tilegroxy.coordinate.y", newRequest.Y),
				attribute.Int("tilegroxy.coordinate.z", newRequest.Z),
			)
		}
	}

	return newCtx, span
}
