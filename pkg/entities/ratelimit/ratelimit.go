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

// Package ratelimit defines the stores that hold rate limit counters. Windows, overrides, and headers are handled by tilegroxy
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

// One count within one window. Every window of a key is counted separately
type RateLimitCounter struct {
	// Identifies the rule and whose usage is counted. Safe to use in any store without escaping
	Key string
	// When the counted window began
	Window time.Time
	// How long from now the count must be kept after an increment
	TTL time.Duration
}

// Only counts. Get and Increment receive each counter at most once per call
type RateLimiter interface {
	// The current value of each counter, in the same order. Zero for one that doesn't exist or has expired
	Get(ctx context.Context, counters []RateLimitCounter) ([]int64, error)
	// Adds one to every counter
	Increment(ctx context.Context, counters []RateLimitCounter) error
}

// Optional. Enables the ratelimit command, so a store that only lives inside the server process shouldn't implement it
type RateLimitResetter interface {
	// Sets each counter back to zero
	Reset(ctx context.Context, counters []RateLimitCounter) error
}

// Optional. A store shared between instances should implement it so they all agree on when windows begin
type Clock interface {
	// The store's current time
	Now(ctx context.Context) (time.Time, error)
}

// New dependencies are added as fields so the Initialize signature stays stable
type RateLimiterDeps struct {
	Datastores    datastore.DatastoreRegistry
	ErrorMessages config.ErrorMessages
}

type RateLimiterRegistration interface {
	Name() string
	Initialize(config any, deps RateLimiterDeps) (RateLimiter, error)
	InitializeConfig() any
}

var registrationsMu sync.RWMutex
var registrations = make(map[string]RateLimiterRegistration)

func RegisterRateLimiter(reg RateLimiterRegistration) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[reg.Name()] = reg
}

func RegisteredRateLimiter(name string) (RateLimiterRegistration, bool) {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	o, ok := registrations[name]
	return o, ok
}

func RegisteredRateLimiterNames() []string {
	registrationsMu.RLock()
	defer registrationsMu.RUnlock()
	names := make([]string, 0, len(registrations))
	for n := range registrations {
		names = append(names, n)
	}
	return names
}
