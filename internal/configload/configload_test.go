// Copyright 2024 Michael Davis
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

package configload

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleYml(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/simple.yml")

	require.NoError(t, err)
	assert.Equal(t, "none", c.Cache.(map[string]interface{})["name"])
}

func TestSimpleJson(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/simple.json")

	require.NoError(t, err)
	assert.Equal(t, "none", c.Cache.(map[string]interface{})["name"])
}

func TestComplexYml(t *testing.T) {
	_, err := LoadConfigFromFile("../../examples/configurations/complex.yml")

	require.NoError(t, err)
}

func TestTwoTierYml(t *testing.T) {
	_, err := LoadConfigFromFile("../../examples/configurations/two_tier_cache.yml")

	require.NoError(t, err)
}

func TestLoadConfig_UnknownTopLevelKeyErrors(t *testing.T) {
	_, err := LoadConfig(`
server:
  producton: true
`)

	require.Error(t, err)
}

func TestLoadConfig_UnknownPortKeyErrors(t *testing.T) {
	_, err := LoadConfig(`
server:
  prot: 9999
`)

	require.Error(t, err)
}

func TestLoadConfig_UnknownLayerKeyErrors(t *testing.T) {
	_, err := LoadConfig(`
layers:
  - id: main
    skipcach: true
    provider:
      name: static
      color: FFF
`)

	require.Error(t, err)
}

func TestLoadConfig_KnownKeysStillWork(t *testing.T) {
	c, err := LoadConfig(`
server:
  production: true
  port: 9999
layers:
  - id: main
    skipCache: true
    provider:
      name: static
      color: FFF
`)

	require.NoError(t, err)
	assert.True(t, c.Server.Production)
	assert.Equal(t, 9999, c.Server.Port)
	assert.True(t, c.Layers[0].SkipCache)
}

func TestDecodeEntityConfig_UnknownKeyErrors(t *testing.T) {
	type fooConfig struct {
		Bar string
	}

	var out fooConfig
	err := DecodeEntityConfig(map[string]interface{}{
		"name": "foo",
		"baz":  "typo'd field",
	}, &out)

	require.Error(t, err)
}

func TestDecodeEntityConfig_NameStrippedButFieldsWork(t *testing.T) {
	type fooConfig struct {
		Bar string
	}

	var out fooConfig
	err := DecodeEntityConfig(map[string]interface{}{
		"name": "foo",
		"bar":  "value",
	}, &out)

	require.NoError(t, err)
	assert.Equal(t, "value", out.Bar)
}

func TestDecodeEntityConfig_IDPassesThroughWhenStructWantsIt(t *testing.T) {
	type withIDConfig struct {
		ID  string
		Bar string
	}

	var out withIDConfig
	err := DecodeEntityConfig(map[string]interface{}{
		"name": "foo",
		"id":   "myid",
		"bar":  "value",
	}, &out)

	require.NoError(t, err)
	assert.Equal(t, "myid", out.ID)
	assert.Equal(t, "value", out.Bar)
}

// AutomaticEnv resolves against viper's key set, so without defaults registered an env var only
// takes effect for a key the config file already contains.
func TestLoadConfig_EnvOverrideWorksForKeyAbsentFromFile(t *testing.T) {
	t.Setenv("SERVER_PORT", "9999")

	c, err := LoadConfig(`
server:
  production: true
`)

	require.NoError(t, err)
	assert.Equal(t, 9999, c.Server.Port)
}

func TestValidate_InvalidErrorMode(t *testing.T) {
	c := config.DefaultConfig()
	c.Error.Mode = "not-a-real-mode"

	err := Validate(c)
	require.Error(t, err)
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	c := config.DefaultConfig()
	c.Logging.Main.Level = "not-a-real-level"

	err := Validate(c)
	require.Error(t, err)
}

func TestValidate_InvalidMainLogFormat(t *testing.T) {
	c := config.DefaultConfig()
	c.Logging.Main.Format = "not-a-real-format"

	err := Validate(c)
	require.Error(t, err)
}

func TestValidate_InvalidAccessLogFormat(t *testing.T) {
	c := config.DefaultConfig()
	c.Logging.Access.Format = "not-a-real-format"

	err := Validate(c)
	require.Error(t, err)
}

func TestValidate_InvalidAuditLogFormat(t *testing.T) {
	c := config.DefaultConfig()
	c.Logging.Audit.Format = "not-a-real-format"

	err := Validate(c)
	require.Error(t, err)
}

func TestValidate_DefaultConfigIsValid(t *testing.T) {
	c := config.DefaultConfig()

	err := Validate(c)
	require.NoError(t, err)
}

func TestValidate_MinZoomAboveMaxZoomRejected(t *testing.T) {
	c := config.DefaultConfig()
	minZoom, maxZoom := 10, 4
	c.Layers = []config.LayerConfig{{ID: "l1", MinZoom: &minZoom, MaxZoom: &maxZoom}}

	err := Validate(c)
	require.Error(t, err)
}

func TestValidate_MinZoomBelowMaxZoomAccepted(t *testing.T) {
	c := config.DefaultConfig()
	minZoom, maxZoom := 4, 10
	c.Layers = []config.LayerConfig{{ID: "l1", MinZoom: &minZoom, MaxZoom: &maxZoom}}

	err := Validate(c)
	require.NoError(t, err)
}

func TestValidate_InvertedBoundsRejected(t *testing.T) {
	c := config.DefaultConfig()
	c.Layers = []config.LayerConfig{{ID: "l1", Bounds: config.BoundsConfig{South: 63, North: 51, West: -10, East: 2}}}

	err := Validate(c)
	require.Error(t, err)
}

func TestValidate_WellFormedBoundsAccepted(t *testing.T) {
	c := config.DefaultConfig()
	c.Layers = []config.LayerConfig{{ID: "l1", Bounds: config.BoundsConfig{South: 51, North: 63, West: -10, East: 2}}}

	err := Validate(c)
	require.NoError(t, err)
}

func TestValidate_UnsetBoundsAccepted(t *testing.T) {
	c := config.DefaultConfig()
	c.Layers = []config.LayerConfig{{ID: "l1"}}

	err := Validate(c)
	require.NoError(t, err)
}

func TestAnalyticsYml(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/analytics.yml")

	require.NoError(t, err)
	assert.Equal(t, "clickhouse", c.Analytics["name"])
}

func TestAnalyticsAsListRejected(t *testing.T) {
	// Viper merges the entries of a list of maps into one map, so without an explicit check this decodes
	// into a silent mixture of the two entries instead of an error.
	_, err := LoadConfig("analytics:\n  - name: clickhouse\n    table: t\n  - name: none\n")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "single entry")
}

func TestAnalyticsDefaultsToNone(t *testing.T) {
	c, err := LoadConfig("layers:\n  - id: osm\n    provider:\n      name: static\n      color: FFF\n")

	require.NoError(t, err)
	assert.Equal(t, "none", c.Analytics["name"])
}

func TestMergeDefaultsFrom(t *testing.T) {
	c1 := config.DefaultConfig().Client

	var c2 config.ClientConfig

	MergeClientDefaults(&c2, c1)

	assert.Equal(t, c1.ContentTypes, c2.ContentTypes)
	assert.Equal(t, c1.Headers, c2.Headers)
	assert.Equal(t, c1.MaxLength, c2.MaxLength)
	assert.Equal(t, c1.RewriteContentTypes, c2.RewriteContentTypes)
	assert.Equal(t, c1.StatusCodes, c2.StatusCodes)
	assert.Equal(t, c1.Timeout, c2.Timeout)
	assert.Equal(t, c1.UnknownLength, c2.UnknownLength)
	assert.Equal(t, c1.UserAgent, c2.UserAgent)

	var c3 config.ClientConfig
	c3.Headers = map[string]string{"test": "test"}

	MergeClientDefaults(&c3, c1)

	assert.Equal(t, c1.ContentTypes, c3.ContentTypes)
	assert.NotEqual(t, c1.Headers, c3.Headers)
	assert.Equal(t, c1.MaxLength, c3.MaxLength)
	assert.Equal(t, c1.RewriteContentTypes, c3.RewriteContentTypes)
	assert.Equal(t, c1.StatusCodes, c3.StatusCodes)
	assert.Equal(t, c1.Timeout, c3.Timeout)
	assert.Equal(t, c1.UnknownLength, c3.UnknownLength)
	assert.Equal(t, c1.UserAgent, c3.UserAgent)
}

func TestMergeDefaultsFrom_UnknownLength(t *testing.T) {
	defaults := config.ClientConfig{UnknownLength: new(true)}

	// A layer explicitly tightening the limit keeps its false.
	explicitFalse := config.ClientConfig{UnknownLength: new(false), Timeout: 5}
	MergeClientDefaults(&explicitFalse, defaults)
	assert.False(t, *explicitFalse.UnknownLength, "an explicit layer-level false must not be overridden by a permissive global default")

	var unset config.ClientConfig
	MergeClientDefaults(&unset, defaults)
	assert.True(t, *unset.UnknownLength, "should inherit default")

	// Unrelated fields still inherit normally.
	assert.Equal(t, uint(0), unset.Timeout)
	assert.Equal(t, uint(5), explicitFalse.Timeout)
}

func Test_ShutdownTimeoutDerivesFromItsPhases(t *testing.T) {
	c := config.DefaultConfig()
	c.Server.Timeout = 45
	c.Server.DrainDelay = 5
	c.Server.ShutdownTimeout = 0

	require.NoError(t, Validate(c))

	// Unset covers both phases that spend it, so the budget always fits a full-length request
	// plus the drain wait.
	assert.Equal(t, uint(50), EffectiveShutdownTimeout(c.Server))
}

func Test_ShutdownTimeoutFitsShortRequestTimeouts(t *testing.T) {
	// A short request timeout with the default drain delay used to be rejected outright, which
	// broke configs that were valid before the drain delay existed.
	c := config.DefaultConfig()
	c.Server.Timeout = 1

	require.NoError(t, Validate(c))
	assert.Equal(t, uint(6), EffectiveShutdownTimeout(c.Server))
}

func Test_ShutdownTimeoutExplicitWins(t *testing.T) {
	c := config.DefaultConfig()
	c.Server.Timeout = 45
	c.Server.ShutdownTimeout = 10

	require.NoError(t, Validate(c))

	assert.Equal(t, uint(10), EffectiveShutdownTimeout(c.Server))
}

func Test_DrainDelayZeroIsValid(t *testing.T) {
	// Zero is meaningful: it means a preStop hook already covered endpoint propagation.
	c := config.DefaultConfig()
	c.Server.DrainDelay = 0

	require.NoError(t, Validate(c))
}

func Test_DrainDelayCannotConsumeWholeBudget(t *testing.T) {
	c := config.DefaultConfig()
	c.Server.ShutdownTimeout = 5
	c.Server.DrainDelay = 5

	// A drain delay at or above the budget leaves no time to actually drain or flush.
	require.Error(t, Validate(c))
}

func TestLoadAndWatchConfigFromFile_ReloadsOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tilegroxy.yml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o600))

	reloads := make(chan config.Config, 1)
	_, err := LoadAndWatchConfigFromFile(path, func(c config.Config, err error) {
		require.NoError(t, err)
		reloads <- c
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8081\n"), 0o600))

	select {
	case c := <-reloads:
		assert.Equal(t, 8081, c.Server.Port)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reload after write")
	}
}

// Regression test for a bug where deleting and recreating the config file (the atomic-save
// pattern used by many editors, `mv`, and Kubernetes ConfigMap symlink swaps) permanently and
// silently killed hot-reload: the underlying fsnotify watch died on the Remove event and was
// never re-armed, so every subsequent save was ignored with no error surfaced anywhere.
func TestLoadAndWatchConfigFromFile_SurvivesDeleteAndRecreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tilegroxy.yml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o600))

	reloads := make(chan config.Config, 1)
	_, err := LoadAndWatchConfigFromFile(path, func(c config.Config, err error) {
		require.NoError(t, err)
		reloads <- c
	})
	require.NoError(t, err)

	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8082\n"), 0o600))

	select {
	case c := <-reloads:
		assert.Equal(t, 8082, c.Server.Port)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reload after delete+recreate: hot-reload appears to have died silently")
	}
}

// A panic anywhere in the watch or reload goroutines (including inside a caller-supplied
// onReload) must be recovered and reported through onReload rather than crashing the process,
// since these goroutines have no other supervisor.
func TestLoadAndWatchConfigFromFile_PanicInOnReloadDoesNotCrash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tilegroxy.yml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o600))

	errs := make(chan error, 1)
	_, err := LoadAndWatchConfigFromFile(path, func(_ config.Config, err error) {
		if err != nil {
			errs <- err
			return
		}
		panic("boom")
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8081\n"), 0o600))

	select {
	case err := <-errs:
		require.ErrorContains(t, err, "boom")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for panic to be reported via onReload")
	}
}

func TestNormalizeCaches_SingleObject(t *testing.T) {
	entries, err := NormalizeCaches(map[string]interface{}{"name": "memory"}, config.DefaultConfig().Error.Messages)

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "memory", entries[0].ID)
	assert.Equal(t, "memory", entries[0].Config["name"])
}

func TestNormalizeCaches_SingleObjectKeepsExplicitID(t *testing.T) {
	entries, err := NormalizeCaches(map[string]interface{}{"name": "memory", "id": "mine"}, config.DefaultConfig().Error.Messages)

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "mine", entries[0].ID)
}

func TestNormalizeCaches_Absent(t *testing.T) {
	entries, err := NormalizeCaches(nil, config.DefaultConfig().Error.Messages)

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "none", entries[0].Config["name"])
}

// A config decoded from YAML or JSON produces []interface{} rather than the []map[string]interface{}
// a Go-built config uses, so both have to normalize the same way.
func TestNormalizeCaches_ArrayForms(t *testing.T) {
	expected := []ConfigWithID{
		{ID: "a", Config: map[string]interface{}{"id": "a", "name": "memory"}},
		{ID: "b", Config: map[string]interface{}{"id": "b", "name": "none"}},
	}

	typed, err := NormalizeCaches([]map[string]interface{}{
		{"id": "a", "name": "memory"},
		{"id": "b", "name": "none"},
	}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)
	assert.Equal(t, expected, typed)

	decoded, err := NormalizeCaches([]interface{}{
		map[string]interface{}{"id": "a", "name": "memory"},
		map[string]interface{}{"id": "b", "name": "none"},
	}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)
	assert.Equal(t, expected, decoded)
}

func TestNormalizeCaches_EmptyArrayFallsBackToNoop(t *testing.T) {
	entries, err := NormalizeCaches([]interface{}{}, config.DefaultConfig().Error.Messages)

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "none", entries[0].Config["name"])
}

func TestNormalizeCaches_IDDefaultsToName(t *testing.T) {
	entries, err := NormalizeCaches([]map[string]interface{}{
		{"name": "memory"},
		{"id": "explicit", "name": "none"},
	}, config.DefaultConfig().Error.Messages)
	require.NoError(t, err)

	require.Len(t, entries, 2)
	assert.Equal(t, "memory", entries[0].ID)
	assert.Equal(t, "explicit", entries[1].ID)
}

func TestNormalizeCaches_Errors(t *testing.T) {
	messages := config.DefaultConfig().Error.Messages

	_, err := NormalizeCaches([]map[string]interface{}{{"maxsize": 10}}, messages)
	require.ErrorContains(t, err, "cache[0].id")

	_, err = NormalizeCaches([]map[string]interface{}{
		{"id": "same", "name": "memory"},
		{"id": "same", "name": "none"},
	}, messages)
	require.ErrorContains(t, err, "same")

	_, err = NormalizeCaches([]interface{}{"not-a-map"}, messages)
	require.ErrorContains(t, err, "cache[0]")

	_, err = NormalizeCaches("nonsense", messages)
	require.ErrorContains(t, err, "cache")
}

// Viper merges a list of maps into one map key by key, so the cache list has to be recovered from
// the raw value. Without that, two caches silently decode into a single mixed-up entry.
func TestLoadConfig_CacheListSurvivesViperMerge(t *testing.T) {
	c, err := LoadConfig(`
cache:
  - id: main
    name: memory
  - id: other
    name: none
layers: []
`)
	require.NoError(t, err)

	entries, err := NormalizeCaches(c.Cache, c.Error.Messages)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "main", entries[0].ID)
	assert.Equal(t, "memory", entries[0].Config["name"])
	assert.Equal(t, "other", entries[1].ID)
	assert.Equal(t, "none", entries[1].Config["name"])
}

func TestLoadConfig_DefaultCacheAndLayerCache(t *testing.T) {
	c, err := LoadConfig(`
cache:
  - id: main
    name: memory
  - id: other
    name: none
defaultcache: other
layers:
  - id: osm
    cache: main
    provider:
      name: proxy
      url: "http://example.com/{z}/{x}/{y}.png"
`)
	require.NoError(t, err)

	assert.Equal(t, "other", c.DefaultCache)
	require.Len(t, c.Layers, 1)
	assert.Equal(t, "main", c.Layers[0].Cache)
}
