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

package layers

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func autoVersion(t *testing.T, provider map[string]any) string {
	t.Helper()

	v, err := resolveCacheVersion(config.LayerConfig{CacheVersion: "auto", Provider: provider}, testErrorMessages)
	require.NoError(t, err)

	return v
}

func Test_ResolveCacheVersion_LiteralPassesThrough(t *testing.T) {
	v, err := resolveCacheVersion(config.LayerConfig{CacheVersion: "v2", Provider: map[string]any{"name": "a"}}, testErrorMessages)
	require.NoError(t, err)
	assert.Equal(t, "v2", v)

	v, err = resolveCacheVersion(config.LayerConfig{}, testErrorMessages)
	require.NoError(t, err)
	assert.Empty(t, v)
}

func Test_ResolveCacheVersion_AutoIsStable(t *testing.T) {
	a := autoVersion(t, map[string]any{"name": "proxy", "url": "http://a", "inverty": true})
	b := autoVersion(t, map[string]any{"inverty": true, "url": "http://a", "name": "proxy"})

	assert.Equal(t, a, b)
	assert.Len(t, a, 17)
}

func Test_ResolveCacheVersion_AutoChangesWithProvider(t *testing.T) {
	a := autoVersion(t, map[string]any{"name": "proxy", "url": "http://a"})
	b := autoVersion(t, map[string]any{"name": "proxy", "url": "http://b"})
	nested := autoVersion(t, map[string]any{"name": "fallback", "primary": map[string]any{"name": "proxy", "url": "http://a"}})

	assert.NotEqual(t, a, b)
	assert.NotEqual(t, a, nested)
}

func autoVersionOf(t *testing.T, cfg config.LayerConfig) string {
	t.Helper()

	cfg.CacheVersion = "auto"
	v, err := resolveCacheVersion(cfg, testErrorMessages)
	require.NoError(t, err)

	return v
}

func Test_ResolveCacheVersion_AutoChangesWithLayerFields(t *testing.T) {
	base := config.LayerConfig{Provider: map[string]any{"name": "proxy"}}
	baseVersion := autoVersionOf(t, base)

	tests := map[string]func(*config.LayerConfig){
		"pattern": func(c *config.LayerConfig) { c.Pattern = "{a}_{b}" },
		"paramMap": func(c *config.LayerConfig) {
			c.ParamMap = map[string][]config.ParamMapEntry{"a": {{From: "x", To: "y"}}}
		},
		"bounds":   func(c *config.LayerConfig) { c.Bounds = config.BoundsConfig{North: 1} },
		"datatype": func(c *config.LayerConfig) { c.DataType = config.DataTypeMVT },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)

			assert.NotEqual(t, baseVersion, autoVersionOf(t, cfg))
		})
	}
}

func Test_ResolveCacheVersion_AutoIgnoresUnrelatedFields(t *testing.T) {
	minZoom := 3
	base := config.LayerConfig{ID: "a", Provider: map[string]any{"name": "proxy"}}
	baseVersion := autoVersionOf(t, base)

	cfg := base
	cfg.ID = "b"
	cfg.ParamValidator = map[string]string{"*": ".*"}
	cfg.Examples = []string{"x"}
	cfg.Cache = "other"
	cfg.RateLimit = "other"
	cfg.MinZoom = &minZoom
	cfg.Description = "desc"
	cfg.Client = &config.ClientConfig{UserAgent: "agent", Headers: map[string]string{"a": "b"}}

	assert.Equal(t, baseVersion, autoVersionOf(t, cfg))
}

func Test_ResolveCacheVersion_AutoIsCaseInsensitive(t *testing.T) {
	provider := map[string]any{"name": "proxy"}
	v, err := resolveCacheVersion(config.LayerConfig{CacheVersion: "AUTO", Provider: provider}, testErrorMessages)
	require.NoError(t, err)

	assert.Equal(t, autoVersion(t, provider), v)
}

func Test_ResolveCacheVersion_UnencodableProviderErrors(t *testing.T) {
	_, err := resolveCacheVersion(config.LayerConfig{CacheVersion: "auto", Provider: map[string]any{"name": make(chan int)}}, testErrorMessages)
	require.Error(t, err)
}

// Secrets and env vars resolve after hashing so rotating them leaves cached tiles valid
func Test_ConstructLayer_AutoCacheVersionIgnoresEnvValues(t *testing.T) {
	layer.RegisterProvider(sampleProviderRegistration{})
	layer.RegisterProvider(fixedTypeTestRegistration{name: "fixed-cacheversion-1", dt: config.DataTypeRaster})

	rawConfig := config.LayerConfig{
		ID:           "test",
		CacheVersion: "auto",
		Provider:     map[string]any{"name": "env.TILEGROXY_CACHEVERSION_TEST"},
	}

	t.Setenv("TILEGROXY_CACHEVERSION_TEST", "sample-provider")
	first, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, config.ErrorMessages{}, nil, nil, nil, nil)
	require.NoError(t, err)

	t.Setenv("TILEGROXY_CACHEVERSION_TEST", "fixed-cacheversion-1")
	second, err := ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, config.ErrorMessages{}, nil, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, first.Config.CacheVersion, second.Config.CacheVersion)
}

func Test_LayerGroup_PurgeTile_AppliesAutoCacheVersion(t *testing.T) {
	layer.RegisterProvider(sampleProviderRegistration{})

	l, err := ConstructLayer(context.Background(), config.LayerConfig{ID: "test", CacheVersion: "auto", Provider: map[string]any{"name": "sample-provider"}}, config.ClientConfig{}, nil, config.ErrorMessages{}, nil, nil, nil, nil)
	require.NoError(t, err)

	c := &removeRecordingCache{}
	lg := purgeTestLayerGroup(t, c, l.Config)

	_, err = lg.PurgeTile(context.Background(), pkg.TileRequest{LayerName: "test", Z: 3, X: 1, Y: 2})
	require.NoError(t, err)

	require.Len(t, c.removed, 1)
	assert.Equal(t, l.Config.CacheVersion+"test", c.removed[0].LayerName)
	assert.NotEqual(t, "autotest", c.removed[0].LayerName)
}
