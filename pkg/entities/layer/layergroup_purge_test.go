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

package layer

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// removeRecordingCache records what PurgeTile asks it to delete.
type removeRecordingCache struct {
	removed []pkg.TileRequest
	present bool
}

func (c *removeRecordingCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c *removeRecordingCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return nil
}

func (c *removeRecordingCache) Remove(_ context.Context, t pkg.TileRequest) (bool, error) {
	c.removed = append(c.removed, t)
	return c.present, nil
}

func purgeTestLayerGroup(t *testing.T, c *removeRecordingCache, cfg config.LayerConfig) *LayerGroup {
	t.Helper()

	l := &Layer{
		ID:       "test",
		Pattern:  []layerSegment{{value: "test", placeholder: false}},
		Provider: &slowGenerateProvider{delay: 0},
		Cache:    c,
		Config:   cfg,
	}
	l.tileAllCounter = noop.Int64Counter{}
	l.tileAuthCounter = noop.Int64Counter{}
	l.tileErrorCounter = noop.Int64Counter{}
	l.tileSuccessCounter = noop.Int64Counter{}

	return &LayerGroup{
		layers:           []*Layer{l},
		DefaultCache:     c,
		cacheHitCounter:  noop.Int64Counter{},
		cacheMissCounter: noop.Int64Counter{},
	}
}

func Test_LayerGroup_PurgeTile_RemovesFromCache(t *testing.T) {
	c := &removeRecordingCache{present: true}
	lg := purgeTestLayerGroup(t, c, config.LayerConfig{})

	req := pkg.TileRequest{LayerName: "test", Z: 3, X: 1, Y: 2}
	removed, err := lg.PurgeTile(context.Background(), req)
	require.NoError(t, err)
	require.True(t, removed)

	require.Equal(t, []pkg.TileRequest{req}, c.removed)
}

// The count a purge reports comes from the cache, so an uncached tile has to come back false.
func Test_LayerGroup_PurgeTile_ReportsUncachedTile(t *testing.T) {
	c := &removeRecordingCache{}
	lg := purgeTestLayerGroup(t, c, config.LayerConfig{})

	removed, err := lg.PurgeTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 3, X: 1, Y: 2})
	require.NoError(t, err)
	require.False(t, removed)
}

// A purge has to target the same key a render would read, which cacheversion prefixes.
func Test_LayerGroup_PurgeTile_AppliesCacheVersion(t *testing.T) {
	c := &removeRecordingCache{}
	lg := purgeTestLayerGroup(t, c, config.LayerConfig{CacheVersion: "v2"})

	_, err := lg.PurgeTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 3, X: 1, Y: 2})
	require.NoError(t, err)

	require.Len(t, c.removed, 1)
	require.Equal(t, "v2test", c.removed[0].LayerName)
}

func Test_LayerGroup_PurgeTile_SkipCacheLayerIsNoop(t *testing.T) {
	c := &removeRecordingCache{}
	lg := purgeTestLayerGroup(t, c, config.LayerConfig{SkipCache: true})

	removed, err := lg.PurgeTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 3, X: 1, Y: 2})
	require.NoError(t, err)
	require.False(t, removed)
	require.Empty(t, c.removed)
}

func Test_LayerGroup_PurgeTile_UnknownLayerErrors(t *testing.T) {
	c := &removeRecordingCache{}
	lg := purgeTestLayerGroup(t, c, config.LayerConfig{})

	_, err := lg.PurgeTile(context.Background(), pkg.TileRequest{LayerName: "missing", Z: 3, X: 1, Y: 2})

	require.Error(t, err)
	require.Empty(t, c.removed)
}

func Test_LayerGroup_PurgeTile_OutOfZoomRangeErrors(t *testing.T) {
	c := &removeRecordingCache{}
	minZoom := 4
	lg := purgeTestLayerGroup(t, c, config.LayerConfig{MinZoom: &minZoom})

	_, err := lg.PurgeTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 1, X: 0, Y: 0})

	var rangeErr pkg.RangeError
	require.ErrorAs(t, err, &rangeErr)
	require.Empty(t, c.removed)
}
