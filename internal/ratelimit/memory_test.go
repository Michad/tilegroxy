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
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMemory(t *testing.T, size uint32) *Memory {
	t.Helper()

	s, err := MemoryRegistration{}.Initialize(MemoryConfig{MaxSize: size}, ratelimit.RateLimiterDeps{})
	require.NoError(t, err)

	return s.(*Memory)
}

func Test_Memory_CountsPerWindow(t *testing.T) {
	m := newMemory(t, 7001)
	ctx := context.Background()
	window := time.Unix(1000, 0)

	counters := []ratelimit.RateLimitCounter{
		{Key: "a", Window: window, TTL: time.Hour},
		{Key: "a", Window: window.Add(time.Minute), TTL: time.Hour},
	}

	require.NoError(t, m.Increment(ctx, counters[:1]))
	require.NoError(t, m.Increment(ctx, counters[:1]))

	counts, err := m.Get(ctx, counters)
	require.NoError(t, err)
	assert.Equal(t, []int64{2, 0}, counts)
}

func Test_Memory_SurvivesReloadWithSameSize(t *testing.T) {
	ctx := context.Background()
	counter := []ratelimit.RateLimitCounter{{Key: "reload", Window: time.Unix(1000, 0), TTL: time.Hour}}

	first := newMemory(t, 7002)
	require.NoError(t, first.Increment(ctx, counter))

	second := newMemory(t, 7002)
	counts, err := second.Get(ctx, counter)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, counts)

	resized := newMemory(t, 7003)
	counts, err = resized.Get(ctx, counter)
	require.NoError(t, err)
	assert.Equal(t, []int64{0}, counts)
}

func Test_Memory_DefaultsAndIsNotInspectable(t *testing.T) {
	assert.Equal(t, MemoryConfig{MaxSize: memoryDefaultMaxSize}, MemoryRegistration{}.InitializeConfig())

	var store ratelimit.RateLimiter = newMemory(t, 7004)
	_, ok := store.(ratelimit.RateLimitResetter)
	assert.False(t, ok)
}
