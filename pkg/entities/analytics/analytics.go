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

// Package analytics defines the contract for recording lightweight usage events when a tile is successfully
// served. This is distinct from telemetry; telemetry aggregates counters for operating the service whereas
// analytics records individual events attributable to a layer, a coordinate and optionally a user
package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/mitchellh/mapstructure"
)

// Event is a single successful tile delivery. Everything but Fields is always populated
type Event struct {
	Time time.Time
	// The ID of the layer as configured, not the name from the URL. Matches how per-layer telemetry
	// metrics are recorded so the two can be correlated
	LayerID string
	// The layer name exactly as requested, which differs from LayerID when the layer uses a pattern
	LayerName string
	Z         int
	X         int
	Y         int
	// The authenticated user, or "" when anonymous. Only the jwt and custom auth modules populate this
	UserID string
	// Additional attributes resolved from the module's configuration. Nil when none are configured
	Fields map[string]any
}

// Analytics receives events for successful tile requests. Implementations must not block the caller; Record
// runs on the request goroutine after the response is written so a slow implementation delays connection
// reuse. Modules talking to a remote system should hand off to a Batcher instead of performing I/O inline.
// Returning an error is for reporting only, it never surfaces to the user or affects the response
type Analytics interface {
	Record(ctx context.Context, event Event) error
}

type AnalyticsRegistration interface {
	Name() string
	Initialize(config any, datastores *datastore.DatastoreRegistry, errorMessages config.ErrorMessages) (Analytics, error)
	InitializeConfig() any
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

var registrations = make(map[string]AnalyticsRegistration)

func RegisterAnalytics(reg AnalyticsRegistration) {
	registrations[reg.Name()] = reg
}

func RegisteredAnalytics(name string) (AnalyticsRegistration, bool) {
	o, ok := registrations[name]
	return o, ok
}

func RegisteredAnalyticsNames() []string {
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}

// noneName is the module that records nothing, used as the default so an absent analytics block behaves
// the same as an explicitly disabled one
const noneName = "none"

func ConstructAnalytics(rawConfig map[string]interface{}, secreter secret.Secreter, datastores *datastore.DatastoreRegistry, errorMessages config.ErrorMessages) (*AnalyticsWrapper, error) {
	var err error

	rawConfig = pkg.ReplaceEnv(rawConfig)

	if secreter != nil {
		rawConfig, err = pkg.ReplaceConfigValues(rawConfig, "secret", secreter.Lookup)
		if err != nil {
			return nil, err
		}
	}

	name, ok := rawConfig["name"].(string)

	if ok {
		reg, ok := RegisteredAnalytics(name)
		if ok {
			cfg := reg.InitializeConfig()
			err := mapstructure.Decode(rawConfig, &cfg)
			if err != nil {
				return nil, err
			}

			// Built from the same raw config the module sees so field validation happens once here
			resolver, err := newFieldResolver(rawConfig, errorMessages)
			if err != nil {
				return nil, err
			}

			a, err := reg.Initialize(cfg, datastores, errorMessages)
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
	return nil, fmt.Errorf(errorMessages.EnumError, "analytics.name", nameCoerce, RegisteredAnalyticsNames())
}
