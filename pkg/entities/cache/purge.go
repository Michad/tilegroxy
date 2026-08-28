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

import "context"

// Purgeable is an optional capability a Cache implementation can add to support deleting all
// entries for a single layer. It isn't part of the Cache interface itself because most backends
// (memcache, and any store that can't enumerate or prefix-delete its own keys) can't implement it
// well, or at all, and requiring it of every cache would be a breaking change for existing
// implementations. Callers that want this behavior should type-assert against Purgeable rather
// than assume every Cache supports it.
type Purgeable interface {
	// Purge deletes every cache entry belonging to layerName. Implementations should treat
	// layerName as untrusted the same way a tile request's layer name is.
	Purge(ctx context.Context, layerName string) error
}

// PurgeIfPurgeable purges layerName from o when o implements Purgeable, and reports via the bool
// whether it did so. A nil o is treated as not purgeable.
func PurgeIfPurgeable(ctx context.Context, o any, layerName string) (bool, error) {
	if o == nil {
		return false, nil
	}

	if p, ok := o.(Purgeable); ok {
		return true, p.Purge(ctx, layerName)
	}

	return false, nil
}
