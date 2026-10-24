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

package sample

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
)

type RateLimitStoreConfig struct {
	Datastore string
}

// Counts in the sample datastore. Policies, windows, and headers are handled by tilegroxy
type RateLimit struct {
	store *sync.Map
}

type rateLimitCount struct {
	mu      sync.Mutex
	count   int64
	expires time.Time
}

func init() {
	ratelimit.RegisterRateLimiter(RateLimitStoreRegistration{})
}

type RateLimitStoreRegistration struct{}

func (RateLimitStoreRegistration) InitializeConfig() any {
	return RateLimitStoreConfig{}
}

func (RateLimitStoreRegistration) Name() string {
	return "sample"
}

func (RateLimitStoreRegistration) Initialize(cfgAny any, deps ratelimit.RateLimiterDeps) (ratelimit.RateLimiter, error) {
	cfg := cfgAny.(RateLimitStoreConfig)

	ds, ok := deps.Datastores.Get(cfg.Datastore)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "ratelimit.datastore", cfg.Datastore)
	}

	store, ok := ds.Native().(*sync.Map)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "ratelimit.datastore", cfg.Datastore)
	}

	return &RateLimit{store: store}, nil
}

func countKey(c ratelimit.RateLimitCounter) string {
	return "ratelimit:" + c.Key + ":" + strconv.FormatInt(c.Window.Unix(), 10)
}

func (r *RateLimit) Get(_ context.Context, counters []ratelimit.RateLimitCounter) ([]int64, error) {
	now := time.Now()
	result := make([]int64, len(counters))

	for i, c := range counters {
		v, ok := r.store.Load(countKey(c))
		if !ok {
			continue
		}

		count := v.(*rateLimitCount)
		count.mu.Lock()

		if now.Before(count.expires) {
			result[i] = count.count
		}

		count.mu.Unlock()
	}

	return result, nil
}

func (r *RateLimit) Increment(_ context.Context, counters []ratelimit.RateLimitCounter) error {
	now := time.Now()

	for _, c := range counters {
		v, _ := r.store.LoadOrStore(countKey(c), &rateLimitCount{})

		count := v.(*rateLimitCount)
		count.mu.Lock()

		if !now.Before(count.expires) {
			count.count = 0
		}

		count.count++
		count.expires = now.Add(c.TTL)

		count.mu.Unlock()
	}

	return nil
}
