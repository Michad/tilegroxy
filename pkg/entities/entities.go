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

// Package entities groups the pluggable entities constructed from a configuration into a single generation
// that can be swapped and released as a unit
package entities

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

// Entities is one fully constructed generation of the pluggable entities described by a configuration. Hot
// reload builds a second generation and swaps it in, so grouping them gives the old one a single place to
// be released
type Entities struct {
	LayerGroup *layer.LayerGroup
	Auth       authentication.Authentication
	Analytics  *analytics.AnalyticsWrapper
	Cache      cache.Cache
	Datastores *datastore.DatastoreRegistry
}

// Close releases every entity holding resources. Providers close first: they hold nothing the analytics
// flush depends on, and a custom provider's close hook should get the deadline while there's still
// budget. Analytics closes next so batched events can still be written through the datastore
// connections they depend on
func (e *Entities) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}

	// Providers and auth go first: neither holds anything the analytics flush needs, and a custom
	// auth script may own resources of its own
	preFlushErr := errors.Join(
		e.LayerGroup.Close(ctx),
		lifecycle.CloseIfCloser(ctx, e.Auth),
	)

	// Sequenced instead of joined in one call because errors.Join evaluates its arguments eagerly. If
	// analytics times out its workers may still be flushing, and closing the pools they write through
	// would turn a slow shutdown into "conn closed" errors on the events shutdown was trying to save.
	// The datastores are left to the process exiting in that case; a leaked pool on a shutdown that
	// already blew its deadline beats corrupting the writes still in progress
	analyticsErr := e.Analytics.Close(ctx)

	if analyticsErr != nil {
		slog.WarnContext(ctx, "Leaving datastore connections open because analytics did not finish flushing: "+analyticsErr.Error())
		return errors.Join(preFlushErr, analyticsErr, lifecycle.CloseIfCloser(ctx, e.Cache))
	}

	return errors.Join(
		preFlushErr,
		lifecycle.CloseIfCloser(ctx, e.Cache),
		e.Datastores.Close(ctx),
	)
}
