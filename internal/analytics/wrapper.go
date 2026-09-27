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
	"log/slog"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/tracing"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/go-viper/mapstructure/v2"
	"go.opentelemetry.io/otel/codes"
)

// AnalyticsWrapper adds tracing to an analytics module. Unlike the cache and provider wrappers it also absorbs
// errors; a broken analytics destination must never degrade tile serving
type AnalyticsWrapper struct {
	Name      string
	ID        string // Identifies this destination in logs. Defaults to the module name
	Analytics analytics.Analytics
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
func (w *AnalyticsWrapper) RecordEvent(ctx context.Context, event analytics.Event, src FieldSource) {
	if w == nil {
		return
	}

	event.Fields = w.resolver.Resolve(ctx, src)

	// Errors are already logged and absorbed by Record
	_ = w.Record(ctx, event)
}

func (w *AnalyticsWrapper) Record(ctx context.Context, event analytics.Event) error {
	newCtx, span := tracing.MakeChildSpan(ctx, nil, "Analytics", w.Name, "Record")
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

// CommonConfig holds the parameters every analytics module accepts. Modules embed it with
// `mapstructure:",squash"` so these keys sit at the top level of the module's configuration
type CommonConfig struct {
	// Identifier used in logs to attribute analytics messages. Defaults to the module name
	ID string
	// Names of additional attributes to include, from the set in event_fields.go. An unrecognized name is a
	// startup error so mistakes surface when running `tilegroxy config check`
	Fields []string
	// Arbitrary additional attributes. Keys are the output attribute names, values select a source via a
	// `ctx.` or `hdr.` prefix and are otherwise used as a literal constant
	ExtraFields map[string]string
	// Controls how events are buffered before being written to the destination
	Batch BatchConfig
}

// noneName is the module that records nothing, used as the default so an absent analytics block behaves
// the same as an explicitly disabled one
const noneName = "none"

// secreter is separate from deps because it resolves values in the raw config before the module is
// constructed, rather than being handed to the module
func ConstructAnalytics(ctx context.Context, rawConfig map[string]interface{}, secreter secret.Secreter, deps analytics.AnalyticsDeps) (*AnalyticsWrapper, error) {
	var err error

	rawConfig = configload.ReplaceEnv(rawConfig)

	if secreter != nil {
		rawConfig, err = configload.ReplaceConfigValues(rawConfig, "secret", func(k string) (string, error) {
			v, _, lookupErr := secreter.Lookup(ctx, k)
			return v, lookupErr
		})
		if err != nil {
			return nil, err
		}
	}

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := analytics.RegisteredAnalytics(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}

			// Built from the same raw config the module sees so field validation happens once here
			resolver, err := newFieldResolver(rawConfig, deps.ErrorMessages)
			if err != nil {
				return nil, err
			}

			a, err := reg.Initialize(cfg, deps)
			if err != nil {
				return nil, err
			}

			id, _ := rawConfig["id"].(string)
			if id == "" {
				id = name
			}

			return &AnalyticsWrapper{Name: name, ID: id, Analytics: a, resolver: resolver}, nil
		}
	}

	nameCoerce := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "analytics.name", nameCoerce, analytics.RegisteredAnalyticsNames())
}
