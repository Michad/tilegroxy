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

package layer_test

import (
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/Michad/tilegroxy/internal/caches"
	_ "github.com/Michad/tilegroxy/internal/providers"
)

func constructWithCache(t *testing.T, rawCache map[string]any, layerCfg config.LayerConfig) *layer.Layer {
	t.Helper()

	errorMessages := config.DefaultConfig().Error.Messages

	builtCache, err := cache.ConstructCache(rawCache, cache.CacheDeps{ErrorMessages: errorMessages})
	require.NoError(t, err)

	if layerCfg.Provider == nil {
		layerCfg.Provider = map[string]any{"name": "static", "color": "FFFFFF"}
	}

	l, err := layer.ConstructLayer(layerCfg, config.DefaultConfig().Client, builtCache, errorMessages, nil, nil, nil)
	require.NoError(t, err)

	return l
}

func Test_CacheControlFacts_ReadsTTLFromCache(t *testing.T) {
	l := constructWithCache(t,
		map[string]any{"name": "ttl", "ttl": 3600, "cache": map[string]any{"name": "memory"}},
		config.LayerConfig{ID: "l"})

	assert.Equal(t, time.Hour, l.CacheControl.TTL)
	assert.False(t, l.CacheControl.Uncacheable)
	assert.False(t, l.CacheControl.PerIdentity)
}

// The TTL can sit anywhere in the chain, so the whole tree is walked to find it
func Test_CacheControlFacts_FindsNestedTTL(t *testing.T) {
	l := constructWithCache(t,
		map[string]any{"name": "tenant", "cache": map[string]any{"name": "ttl", "ttl": 120, "cache": map[string]any{"name": "memory"}}},
		config.LayerConfig{ID: "l"})

	assert.Equal(t, 120*time.Second, l.CacheControl.TTL)
	assert.True(t, l.CacheControl.PerIdentity)
}

// The innermost expiry is what actually ends the tile's life
func Test_CacheControlFacts_ShortestNestedTTLWins(t *testing.T) {
	l := constructWithCache(t,
		map[string]any{"name": "ttl", "ttl": 3600, "cache": map[string]any{"name": "ttl", "ttl": 60, "cache": map[string]any{"name": "memory"}}},
		config.LayerConfig{ID: "l"})

	assert.Equal(t, time.Minute, l.CacheControl.TTL)
}

func Test_CacheControlFacts_NoTTLLeavesZero(t *testing.T) {
	l := constructWithCache(t, map[string]any{"name": "memory"}, config.LayerConfig{ID: "l"})

	assert.Zero(t, l.CacheControl.TTL)
	assert.False(t, l.CacheControl.Uncacheable)
}

func Test_CacheControlFacts_NoneCacheIsUncacheable(t *testing.T) {
	l := constructWithCache(t, map[string]any{"name": "none"}, config.LayerConfig{ID: "l"})

	assert.True(t, l.CacheControl.Uncacheable)
}

func Test_CacheControlFacts_SkipCacheIsUncacheable(t *testing.T) {
	l := constructWithCache(t,
		map[string]any{"name": "ttl", "ttl": 3600, "cache": map[string]any{"name": "memory"}},
		config.LayerConfig{ID: "l", SkipCache: true})

	assert.True(t, l.CacheControl.Uncacheable)
	assert.Zero(t, l.CacheControl.TTL)
}

func Test_CacheControlFacts_TenantCacheIsPerIdentity(t *testing.T) {
	l := constructWithCache(t,
		map[string]any{"name": "tenant", "cache": map[string]any{"name": "memory"}},
		config.LayerConfig{ID: "l"})

	assert.True(t, l.CacheControl.PerIdentity)
}

func Test_CacheControlFacts_IdentityPlaceholderIsPerIdentity(t *testing.T) {
	for _, placeholder := range []string{"{ctx.user}", "{ctx.tenant}"} {
		t.Run(placeholder, func(t *testing.T) {
			l := constructWithCache(t,
				map[string]any{"name": "memory"},
				config.LayerConfig{ID: "l", Provider: map[string]any{
					"name": "proxy",
					"url":  "https://example.com/" + placeholder + "/{z}/{x}/{y}.png",
				}})

			assert.True(t, l.CacheControl.PerIdentity)
		})
	}
}

// The documented limit of the detection: another way of varying per caller isn't seen
func Test_CacheControlFacts_UndetectedPerCallerVariationStaysPublic(t *testing.T) {
	l := constructWithCache(t,
		map[string]any{"name": "memory"},
		config.LayerConfig{ID: "l", Provider: map[string]any{
			"name": "proxy",
			"url":  "https://example.com/{z}/{x}/{y}.png?key={ctx.X-Api-Key}",
		}})

	assert.False(t, l.CacheControl.PerIdentity)
}
