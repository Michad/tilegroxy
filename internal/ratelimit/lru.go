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
	"container/list"
	"sync"
	"time"
)

// Counts that expire, dropping the least recently used when full
type lru struct {
	mu    sync.Mutex
	size  int
	items map[string]*list.Element
	order *list.List
}

type lruEntry struct {
	key     string
	count   int64
	expires time.Time
}

func newLRU(size int) *lru {
	size = max(size, 1)

	return &lru{size: size, items: make(map[string]*list.Element, size), order: list.New()}
}

// Zero when missing or expired
func (c *lru) get(key string, now time.Time) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return 0
	}

	entry := el.Value.(*lruEntry)
	if !now.Before(entry.expires) {
		c.order.Remove(el)
		delete(c.items, key)

		return 0
	}

	c.order.MoveToFront(el)

	return entry.count
}

// Returns the new count. An expired count starts over
func (c *lru) add(key string, now time.Time, ttl time.Duration) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	expires := now.Add(ttl)

	if el, ok := c.items[key]; ok {
		entry := el.Value.(*lruEntry)

		if now.Before(entry.expires) {
			entry.count++
		} else {
			entry.count = 1
		}

		if expires.After(entry.expires) {
			entry.expires = expires
		}

		c.order.MoveToFront(el)

		return entry.count
	}

	for c.order.Len() >= c.size {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*lruEntry).key)
	}

	c.items[key] = c.order.PushFront(&lruEntry{key: key, count: 1, expires: expires})

	return 1
}

func (c *lru) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.order.Remove(el)
		delete(c.items, key)
	}
}
