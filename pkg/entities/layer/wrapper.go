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

package layer

import (
	"context"

	"github.com/Michad/tilegroxy/pkg"
	"go.opentelemetry.io/otel/codes"
)

// A struct that wraps all other providers in order to add in instrumentation, specifically child spans for tracing the flow between providers. This is used even when telemetry is disabled but OTEL handles no-op'ing in that case so performance impact is minimal
type ProviderWrapper struct {
	Name     string
	Provider Provider
}

func (t ProviderWrapper) PreAuth(ctx context.Context, providerContext ProviderContext) (ProviderContext, error) {
	newCtx, span := pkg.MakeChildSpan(ctx, nil, "Provider", t.Name, "PreAuth")
	defer span.End()

	pc, err := t.Provider.PreAuth(newCtx, providerContext)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+t.Name)
	}

	return pc, err
}

func (t ProviderWrapper) GenerateTile(ctx context.Context, providerContext ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	newCtx, span := pkg.MakeChildSpan(ctx, &tileRequest, "Provider", t.Name, "GenerateTile")
	defer span.End()

	img, err := t.Provider.GenerateTile(newCtx, providerContext, tileRequest)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+t.Name)
	}

	return img, err
}
