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

package tg

import (
	"errors"
	"fmt"
	"io"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
)

type PurgeOptions struct {
	LayerName string
}

// PurgeCache deletes every cache entry for a single layer. This only works for cache backends
// that implement cache.Purgeable; most backends don't, either because they can't enumerate their
// own keys (e.g. memcache) or because doing so safely/efficiently isn't practical, so a cache
// that doesn't support it is reported rather than treated as an error.
func PurgeCache(cfg *config.Config, opts PurgeOptions, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}

	if opts.LayerName == "" {
		return errors.New("layer name is required")
	}

	ctx := pkg.BackgroundContext()

	ent, err := configToEntities(*cfg)
	if err != nil {
		return err
	}

	defer ent.Close(ctx) //nolint:errcheck // Nothing actionable while tearing down a one-off command

	if ent.LayerGroup.FindLayer(ctx, opts.LayerName) == nil {
		return errors.New("invalid layer")
	}

	// ent.Cache is always a CacheWrapper (see cache.ConstructCache), which deliberately doesn't
	// implement Purgeable itself since it wraps every backend regardless of whether it supports
	// purging. Check the wrapped cache so the type assertion reflects the actual backend.
	target := any(ent.Cache)
	if wrapper, ok := ent.Cache.(cache.CacheWrapper); ok {
		target = wrapper.Cache
	}

	purged, err := cache.PurgeIfPurgeable(ctx, target, opts.LayerName)
	if err != nil {
		return err
	}

	if !purged {
		fmt.Fprintf(out, "The configured cache doesn't support purging by layer. Nothing was deleted.\n")
		return nil
	}

	fmt.Fprintf(out, "Purged layer %q from cache\n", opts.LayerName)
	return nil
}
