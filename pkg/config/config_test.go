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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleYml(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/simple.yml")

	require.NoError(t, err)
	assert.Equal(t, "none", c.Cache["name"])
}

func TestSimpleJson(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/simple.json")

	require.NoError(t, err)
	assert.Equal(t, "none", c.Cache["name"])
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

// Proxying a vector tile source is a documented use case, so the default allowlist has to cover
// the MVT content types and not just raster ones.
func TestDefaultConfig_ContentTypesIncludesVectorTileTypes(t *testing.T) {
	c := DefaultConfig()

	assert.Contains(t, c.Client.ContentTypes, "application/vnd.mapbox-vector-tile")
	assert.Contains(t, c.Client.ContentTypes, "application/x-protobuf")
}

func TestValidate_InvalidErrorMode(t *testing.T) {
	c := DefaultConfig()
	c.Error.Mode = "not-a-real-mode"

	err := c.Validate()
	require.Error(t, err)
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	c := DefaultConfig()
	c.Logging.Main.Level = "not-a-real-level"

	err := c.Validate()
	require.Error(t, err)
}

func TestValidate_InvalidMainLogFormat(t *testing.T) {
	c := DefaultConfig()
	c.Logging.Main.Format = "not-a-real-format"

	err := c.Validate()
	require.Error(t, err)
}

func TestValidate_InvalidAccessLogFormat(t *testing.T) {
	c := DefaultConfig()
	c.Logging.Access.Format = "not-a-real-format"

	err := c.Validate()
	require.Error(t, err)
}

func TestValidate_DefaultConfigIsValid(t *testing.T) {
	c := DefaultConfig()

	err := c.Validate()
	require.NoError(t, err)
}

func TestValidate_MinZoomAboveMaxZoomRejected(t *testing.T) {
	c := DefaultConfig()
	minZoom, maxZoom := 10, 4
	c.Layers = []LayerConfig{{ID: "l1", MinZoom: &minZoom, MaxZoom: &maxZoom}}

	err := c.Validate()
	require.Error(t, err)
}

func TestValidate_MinZoomBelowMaxZoomAccepted(t *testing.T) {
	c := DefaultConfig()
	minZoom, maxZoom := 4, 10
	c.Layers = []LayerConfig{{ID: "l1", MinZoom: &minZoom, MaxZoom: &maxZoom}}

	err := c.Validate()
	require.NoError(t, err)
}

func TestValidate_InvertedBoundsRejected(t *testing.T) {
	c := DefaultConfig()
	c.Layers = []LayerConfig{{ID: "l1", Bounds: BoundsConfig{South: 63, North: 51, West: -10, East: 2}}}

	err := c.Validate()
	require.Error(t, err)
}

func TestValidate_WellFormedBoundsAccepted(t *testing.T) {
	c := DefaultConfig()
	c.Layers = []LayerConfig{{ID: "l1", Bounds: BoundsConfig{South: 51, North: 63, West: -10, East: 2}}}

	err := c.Validate()
	require.NoError(t, err)
}

func TestValidate_UnsetBoundsAccepted(t *testing.T) {
	c := DefaultConfig()
	c.Layers = []LayerConfig{{ID: "l1"}}

	err := c.Validate()
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
	c1 := DefaultConfig().Client

	var c2 ClientConfig

	c2.MergeDefaultsFrom(c1)

	assert.Equal(t, c1.ContentTypes, c2.ContentTypes)
	assert.Equal(t, c1.Headers, c2.Headers)
	assert.Equal(t, c1.MaxLength, c2.MaxLength)
	assert.Equal(t, c1.RewriteContentTypes, c2.RewriteContentTypes)
	assert.Equal(t, c1.StatusCodes, c2.StatusCodes)
	assert.Equal(t, c1.Timeout, c2.Timeout)
	assert.Equal(t, c1.UnknownLength, c2.UnknownLength)
	assert.Equal(t, c1.UserAgent, c2.UserAgent)

	var c3 ClientConfig
	c3.Headers = map[string]string{"test": "test"}

	c3.MergeDefaultsFrom(c1)

	assert.Equal(t, c1.ContentTypes, c3.ContentTypes)
	assert.NotEqual(t, c1.Headers, c3.Headers)
	assert.Equal(t, c1.MaxLength, c3.MaxLength)
	assert.Equal(t, c1.RewriteContentTypes, c3.RewriteContentTypes)
	assert.Equal(t, c1.StatusCodes, c3.StatusCodes)
	assert.Equal(t, c1.Timeout, c3.Timeout)
	assert.Equal(t, c1.UnknownLength, c3.UnknownLength)
	assert.Equal(t, c1.UserAgent, c3.UserAgent)
}

// UnknownLength is a plain bool, so a layer setting `unknownlength: false` to tighten a permissive
// global default is indistinguishable from one that left it unset. Inheriting could only ever be
// observed overriding that explicit false, so MergeDefaultsFrom leaves the field alone.
func TestMergeDefaultsFrom_UnknownLength(t *testing.T) {
	defaults := ClientConfig{UnknownLength: true}

	// A layer explicitly tightening the limit keeps its false.
	explicitFalse := ClientConfig{UnknownLength: false, Timeout: 5}
	explicitFalse.MergeDefaultsFrom(defaults)
	assert.False(t, explicitFalse.UnknownLength, "an explicit layer-level false must not be overridden by a permissive global default")

	// A layer that says nothing also stays false, since unset is not distinguishable from false.
	var unset ClientConfig
	unset.MergeDefaultsFrom(defaults)
	assert.False(t, unset.UnknownLength, "unset is indistinguishable from an explicit false, so it stays false rather than silently loosening the limit")

	// Unrelated fields still inherit normally.
	assert.Equal(t, uint(0), unset.Timeout)
	assert.Equal(t, uint(5), explicitFalse.Timeout)
}

func Test_ShutdownTimeoutDerivesFromItsPhases(t *testing.T) {
	c := DefaultConfig()
	c.Server.Timeout = 45
	c.Server.DrainDelay = 5
	c.Server.ShutdownTimeout = 0

	require.NoError(t, c.Validate())

	// Unset covers both phases that spend it, so the budget always fits a full-length request
	// plus the drain wait.
	assert.Equal(t, uint(50), c.Server.EffectiveShutdownTimeout())
}

func Test_ShutdownTimeoutFitsShortRequestTimeouts(t *testing.T) {
	// A short request timeout with the default drain delay used to be rejected outright, which
	// broke configs that were valid before the drain delay existed.
	c := DefaultConfig()
	c.Server.Timeout = 1

	require.NoError(t, c.Validate())
	assert.Equal(t, uint(6), c.Server.EffectiveShutdownTimeout())
}

func Test_ShutdownTimeoutExplicitWins(t *testing.T) {
	c := DefaultConfig()
	c.Server.Timeout = 45
	c.Server.ShutdownTimeout = 10

	require.NoError(t, c.Validate())

	assert.Equal(t, uint(10), c.Server.EffectiveShutdownTimeout())
}

func Test_DrainDelayDefaultsToFive(t *testing.T) {
	c := DefaultConfig()

	assert.Equal(t, uint(5), c.Server.DrainDelay)
}

func Test_DrainDelayZeroIsValid(t *testing.T) {
	// Zero is meaningful: it means a preStop hook already covered endpoint propagation.
	c := DefaultConfig()
	c.Server.DrainDelay = 0

	require.NoError(t, c.Validate())
}

func Test_DrainDelayCannotConsumeWholeBudget(t *testing.T) {
	c := DefaultConfig()
	c.Server.ShutdownTimeout = 5
	c.Server.DrainDelay = 5

	// A drain delay at or above the budget leaves no time to actually drain or flush.
	require.Error(t, c.Validate())
}

func Test_LayerConfig_HasDataTypeZoomBoundsFields(t *testing.T) {
	minZoom := 4
	maxZoom := 18
	cfg := LayerConfig{
		DataType: DataTypeRaster,
		MinZoom:  &minZoom,
		MaxZoom:  &maxZoom,
		Bounds:   BoundsConfig{South: -10, North: 10, West: -10, East: 10},
	}

	assert.Equal(t, DataTypeRaster, cfg.DataType)
	assert.Equal(t, 4, *cfg.MinZoom)
	assert.Equal(t, 18, *cfg.MaxZoom)
	assert.InDelta(t, 10.0, cfg.Bounds.North, 0)
}

func Test_DataType_Constants_Values(t *testing.T) {
	assert.Equal(t, DataTypeRaster, DataType("raster"))
	assert.Equal(t, DataTypeMVT, DataType("mvt"))
	assert.Equal(t, DataTypeUnknown, DataType("unknown"))
}
