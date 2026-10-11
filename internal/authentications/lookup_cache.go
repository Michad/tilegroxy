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
	"log/slog"
	"time"

	"github.com/maypok86/otter"
	"k8s.io/utils/keymutex"
)

// A nil lookupCache is valid and calls the lookup every time. A zero TTL never expires entries
type lookupCache[T any] struct {
	cache otter.Cache[string, T]
	// So a page of tiles sharing a credential does one lookup
	locks keymutex.KeyMutex
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

	return &lookupCache[T]{cache: cache, locks: keymutex.NewHashed(-1)}, nil
}

// Errors aren't cached, so an outage doesn't lock out valid credentials once it recovers
func (c *lookupCache[T]) get(ctx context.Context, key string, lookup func() (T, error)) (T, error) {
	if c == nil {
		return lookup()
	}

	c.locks.LockKey(key)
	defer func() {
		if err := c.locks.UnlockKey(key); err != nil {
			slog.ErrorContext(ctx, "Unable to release auth lock - this credential might encounter issues going forward", "error", err)
		}
	}()

	if result, ok := c.cache.Get(key); ok {
		return result, nil
	}

	result, err := lookup()
	if err != nil {
		return result, err
	}

	c.cache.Set(key, result)

	return result, nil
}

// A TTL cache runs a cleanup goroutine that would otherwise leak on every reload
func (c *lookupCache[T]) close() {
	if c != nil {
		c.cache.Close()
	}
}
