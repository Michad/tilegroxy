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

package layers

import (
	"context"

	"github.com/Michad/tilegroxy/internal/tracing"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"go.opentelemetry.io/otel/codes"
)

// Adds tracing spans around every provider. Always used since OTEL no-ops when telemetry is disabled
type ProviderWrapper struct {
	Name     string
	Provider layer.Provider
	dataType config.DataType // As declared by the provider's registration
}

func (t ProviderWrapper) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	newCtx, span := tracing.MakeChildSpan(ctx, nil, "Provider", t.Name, "PreAuth")
	defer span.End()

	pc, err := t.Provider.PreAuth(newCtx, providerContext)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+t.Name)
	}

	return pc, err
}

func (t ProviderWrapper) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	newCtx, span := tracing.MakeChildSpan(ctx, &tileRequest, "Provider", t.Name, "GenerateTile")
	defer span.End()

	img, err := t.Provider.GenerateTile(newCtx, providerContext, tileRequest)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Error from "+t.Name)
	}

	return img, err
}

// Closes the whole tree beneath, so the wrapper is deliberately not a Parent
func (t ProviderWrapper) Close(ctx context.Context) error {
	return layer.CloseProvider(ctx, t.Provider)
}

func (t ProviderWrapper) Metadata() layer.Description {
	d := layer.DescribeTree(t.Provider)
	if isKnownDataType(t.dataType) {
		d.DataType = t.dataType
	}

	return d
}
