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

// Package analytics records individual usage events per layer, coordinate and user, unlike telemetry's aggregate counters
package analytics

import (
	"context"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

// A single successful tile delivery. Everything but Fields is always populated
type Event struct {
	Time time.Time
	// The configured ID, not the URL name. Matches per-layer telemetry metrics so the two can be correlated
	LayerID string
	// Exactly as requested, which differs from LayerID for pattern layers
	LayerName string
	Z         int
	X         int
	Y         int
	// Empty when anonymous. Only the jwt and custom auth modules populate this
	UserID string
	// Resolved from the module's configuration. Nil when none are configured
	Fields map[string]any
}

// Record runs on the request goroutine after the response, so must not block. Errors are only reported, never surfaced
type Analytics interface {
	Record(ctx context.Context, event Event) error
}

// New dependencies are added as fields so the Initialize signature stays stable
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
