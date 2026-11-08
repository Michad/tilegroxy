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

package authentications

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/maypok86/otter"
)

// A nil lookupCache is valid and calls the lookup every time. A zero TTL never expires entries
type lookupCache[T any] struct {
	cache otter.Cache[string, T]
	// So a page of tiles sharing a credential does one lookup
	mu      sync.Mutex
	flights map[string]*lookupFlight[T]
	isStale func(T) bool
}

type lookupFlight[T any] struct {
	done   chan struct{}
	result T
	err    error
	// The leader's own cancellation or panic says nothing about the credential, so waiters retry
	abandoned bool
}

func newLookupCache[T any](size int, ttlSeconds uint32) (*lookupCache[T], error) {
	if size <= 0 {
		return nil, nil
	}

	builder := otter.MustBuilder[string, T](size)

	var cache otter.Cache[string, T]
	var err error
	if ttlSeconds == 0 {
		cache, err = builder.Build()
	} else {
		cache, err = builder.WithTTL(time.Duration(ttlSeconds) * time.Second).Build()
	}
	if err != nil {
		return nil, err
	}

	return &lookupCache[T]{cache: cache, flights: make(map[string]*lookupFlight[T])}, nil
}

// For entries whose lifetime comes from the cached value itself rather than a fixed TTL
func (c *lookupCache[T]) withStaleCheck(isStale func(T) bool) *lookupCache[T] {
	if c != nil {
		c.isStale = isStale
	}

	return c
}

// Errors aren't cached, so an outage doesn't lock out valid credentials once it recovers
func (c *lookupCache[T]) get(ctx context.Context, rawKey string, lookup func() (T, error)) (T, error) {
	if c == nil {
		return lookup()
	}

	key := lookupCacheKey(rawKey)

	for {
		if result, ok := c.cached(key); ok {
			return result, nil
		}

		c.mu.Lock()
		f, inFlight := c.flights[key]
		if !inFlight {
			f = &lookupFlight[T]{done: make(chan struct{}), abandoned: true}
			c.flights[key] = f
		}
		c.mu.Unlock()

		if !inFlight {
			return c.lead(ctx, key, f, lookup)
		}

		select {
		case <-f.done:
			if !f.abandoned {
				return f.result, f.err
			}
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
}

// Runs on the caller's goroutine so lookups that write to the request context only touch the leader's
func (c *lookupCache[T]) lead(ctx context.Context, key string, f *lookupFlight[T], lookup func() (T, error)) (T, error) {
	defer func() {
		c.mu.Lock()
		delete(c.flights, key)
		c.mu.Unlock()
		close(f.done)
	}()

	// A flight may have finished between the caller's cache check and claiming this one
	if result, ok := c.cached(key); ok {
		f.result, f.abandoned = result, false
		return result, nil
	}

	f.result, f.err = lookup()
	f.abandoned = f.err != nil && ctx.Err() != nil
	if f.err == nil {
		c.cache.Set(key, f.result)
	}

	return f.result, f.err
}

// Hashed to bound the memory hostile credentials can make the cache hold
func lookupCacheKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return string(sum[:])
}

func (c *lookupCache[T]) cached(key string) (T, bool) {
	result, ok := c.cache.Get(key)
	if !ok || (c.isStale != nil && c.isStale(result)) {
		var zero T
		return zero, false
	}

	return result, true
}

// A TTL cache runs a cleanup goroutine that would otherwise leak on every reload
func (c *lookupCache[T]) close() {
	if c != nil {
		c.cache.Close()
	}
}
