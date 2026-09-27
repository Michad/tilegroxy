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
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
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
// reuse. Modules talking to a remote system should queue events and write them in the background.
// Returning an error is for reporting only, it never surfaces to the user or affects the response
type Analytics interface {
	Record(ctx context.Context, event Event) error
}

// AnalyticsDeps carries everything an analytics module is given at construction. New dependencies are
// added as fields so the Initialize signature stays stable
type AnalyticsDeps struct {
	Datastores    datastore.DatastoreRegistry
	ErrorMessages config.ErrorMessages
}

type AnalyticsRegistration interface {
	Name() string
	Initialize(config any, deps AnalyticsDeps) (Analytics, error)
	InitializeConfig() any
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]AnalyticsRegistration)

func RegisterAnalytics(reg AnalyticsRegistration) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[reg.Name()] = reg
}

func RegisteredAnalytics(name string) (AnalyticsRegistration, bool) {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	o, ok := registrations[name]
	return o, ok
}

func RegisteredAnalyticsNames() []string {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}
