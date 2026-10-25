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

package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
)

const memoryDefaultMaxSize = 100000

type MemoryConfig struct {
	MaxSize uint32 // Maximum number of counters to hold. Defaults to 100000
}

// Deliberately not a Resetter, since the ratelimit command runs in a different process
type Memory struct {
	counters *lru
	now      func() time.Time
}

// Lets counters survive a reload, which builds a new store, as long as the size is unchanged
var memoryShared struct {
	mu       sync.Mutex
	size     uint32
	counters *lru
}

func init() {
	ratelimit.RegisterRateLimiter(MemoryRegistration{})
}

type MemoryRegistration struct{}

func (MemoryRegistration) InitializeConfig() any {
	return MemoryConfig{MaxSize: memoryDefaultMaxSize}
}

func (MemoryRegistration) Name() string {
	return "memory"
}

func (MemoryRegistration) Initialize(cfgAny any, deps ratelimit.RateLimiterDeps) (ratelimit.RateLimiter, error) {
	cfg := cfgAny.(MemoryConfig)

	if cfg.MaxSize == 0 {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "ratelimit.memory.maxsize", cfg.MaxSize)
	}

	memoryShared.mu.Lock()
	defer memoryShared.mu.Unlock()

	if memoryShared.counters == nil || memoryShared.size != cfg.MaxSize {
		memoryShared.counters = newLRU(int(cfg.MaxSize))
		memoryShared.size = cfg.MaxSize
	}

	return &Memory{counters: memoryShared.counters, now: time.Now}, nil
}

func memoryKey(c ratelimit.RateLimitCounter) string {
	return c.Key + "@" + strconv.FormatInt(c.Window.Unix(), 10)
}

func (m *Memory) Get(_ context.Context, counters []ratelimit.RateLimitCounter) ([]int64, error) {
	now := m.now()
	result := make([]int64, len(counters))

	for i, c := range counters {
		result[i] = m.counters.get(memoryKey(c), now)
	}

	return result, nil
}

func (m *Memory) Increment(_ context.Context, counters []ratelimit.RateLimitCounter) error {
	now := m.now()

	for _, c := range counters {
		m.counters.add(memoryKey(c), now, c.TTL)
	}

	return nil
}
