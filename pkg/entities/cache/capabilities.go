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

import "time"

// Caches that nest others. Children returns them in configuration order
type Parent interface {
	Children() []Cache
}

// Caches that stop returning a tile once it reaches a maximum age
type Expiring interface {
	TTL() time.Duration
}

// Caches whose entries vary by requester
type PerIdentity interface {
	KeyedByIdentity() bool
}

// Caches that never retain a tile
type Noop interface {
	IsNoop() bool
}

// Caches wrapping another without storing anything, so capability checks see through them
type Decorator interface {
	Unwrap() Cache
}
