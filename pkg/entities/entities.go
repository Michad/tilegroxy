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

// Package entities groups the pluggable entities constructed from a configuration into a single generation that can be swapped and released as a unit
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

// One fully constructed generation of the pluggable entities described by a configuration. Hot reload builds a second generation and swaps it in, so grouping them gives the old one a single place to be released
type Entities struct {
	LayerGroup *layer.LayerGroup
	Auth       authentication.Authentication
	Analytics  *analytics.AnalyticsWrapper
	Caches     *cache.CacheRegistry
	Datastores *datastore.DatastoreRegistry
}

// Close releases every entity holding resources.
func (e *Entities) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}

	preFlushErr := errors.Join(
		e.LayerGroup.Close(ctx),
		lifecycle.CloseIfCloser(ctx, e.Auth),
	)

	analyticsErr := e.Analytics.Close(ctx)

	cacheWriteErr := e.LayerGroup.WaitForCacheWrites(ctx)

	if analyticsErr != nil {
		slog.WarnContext(ctx, "Leaving datastore connections open because analytics did not finish flushing: "+analyticsErr.Error())
		return errors.Join(preFlushErr, analyticsErr, cacheWriteErr, e.Caches.Close(ctx))
	}

	return errors.Join(
		preFlushErr,
		cacheWriteErr,
		e.Caches.Close(ctx),
		e.Datastores.Close(ctx),
	)
}
