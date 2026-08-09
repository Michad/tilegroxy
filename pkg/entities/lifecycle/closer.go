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

// Package lifecycle contains the teardown contract shared between entity types. It's a leaf package so any
// entity package can depend on it without creating a cycle
package lifecycle

import (
	"context"
)

// Closer is optionally implemented by any entity holding resources that need to be released on shutdown or
// hot reload. Entities aren't required to implement this, use CloseIfCloser instead of type asserting at
// each call site. Implementations must be safe to call more than once and should respect cancellation of
// the supplied context which carries the deadline allowed for shutdown
type Closer interface {
	Close(ctx context.Context) error
}

// CloseIfCloser closes o if it implements Closer and does nothing otherwise. A nil o is ignored
func CloseIfCloser(ctx context.Context, o any) error {
	if o == nil {
		return nil
	}

	if c, ok := o.(Closer); ok {
		return c.Close(ctx)
	}

	return nil
}
