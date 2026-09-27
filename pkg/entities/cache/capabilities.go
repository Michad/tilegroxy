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

package cache

import (
	"reflect"
	"time"
)

// Parent is implemented by caches that nest others. Children returns what ConstructCache built, in configuration order.
type Parent interface {
	Children() []Cache
}

// Expiring is implemented by caches that stop returning a tile once it reaches a maximum age.
type Expiring interface {
	TTL() time.Duration
}

// PerIdentity is implemented by caches whose entries vary by who made the request.
type PerIdentity interface {
	KeyedByIdentity() bool
}

// Noop is implemented by caches that never retain a tile.
type Noop interface {
	IsNoop() bool
}

// Decorator is implemented by caches wrapping another without storing anything, so capability checks see through them.
type Decorator interface {
	Unwrap() Cache
}

// IsKeyedByIdentity reports whether any cache in the tree keys its entries by the requester.
func IsKeyedByIdentity(c Cache) bool {
	found := false

	walk(c, func(node Cache) {
		if p, ok := as[PerIdentity](node); ok && p.KeyedByIdentity() {
			found = true
		}
	})

	return found
}

// IsNoop reports whether the cache never retains a tile. A Parent of noop caches is not itself a noop.
func IsNoop(c Cache) bool {
	n, ok := as[Noop](c)
	return ok && n.IsNoop()
}

// as finds the first cache implementing T, starting at c and following Decorators inward.
func as[T any](c Cache) (T, bool) {
	for c != nil {
		if found, ok := c.(T); ok {
			return found, true
		}

		d, ok := c.(Decorator)
		if !ok {
			break
		}

		c = d.Unwrap()
	}

	var zero T

	return zero, false
}

// walk calls visit on c and every cache nested below it, depth first in configuration order.
func walk(c Cache, visit func(Cache)) {
	if c == nil {
		return
	}

	visit(c)

	for _, child := range children(c) {
		walk(child, visit)
	}
}

func children(c Cache) []Cache {
	if p, ok := as[Parent](c); ok {
		return p.Children()
	}

	return reflectedChildren(innermost(c))
}

func innermost(c Cache) Cache {
	for {
		d, ok := c.(Decorator)
		if !ok {
			return c
		}

		c = d.Unwrap()
	}
}

// Keeps nesting caches written before Parent existed discoverable, notably ones wrapping a tenant cache.
func reflectedChildren(c Cache) []Cache {
	value := reflect.ValueOf(c)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}

		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return nil
	}

	if tiers := value.FieldByName("Tiers"); tiers.IsValid() && tiers.Kind() == reflect.Slice && tiers.CanInterface() {
		result := make([]Cache, 0, tiers.Len())

		for i := range tiers.Len() {
			if child, ok := tiers.Index(i).Interface().(Cache); ok {
				result = append(result, child)
			}
		}

		return result
	}

	if inner := value.FieldByName("Cache"); inner.IsValid() && inner.CanInterface() {
		if child, ok := inner.Interface().(Cache); ok {
			return []Cache{child}
		}
	}

	return nil
}
