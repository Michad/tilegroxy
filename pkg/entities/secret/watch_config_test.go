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

package secret

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testErrorMessages() config.ErrorMessages {
	return config.ErrorMessages{
		InvalidParam:            "Invalid value supplied for parameter %v: %v",
		RangeError:              "%v must be between %v and %v",
		ParamsMutuallyExclusive: "Parameters %v and %v cannot both be set",
	}
}

func Test_ParseWatchConfig_DefaultsToDisabled(t *testing.T) {
	cfg, stripped, err := parseWatchConfig(map[string]interface{}{"name": "x"}, 1, testErrorMessages())
	require.NoError(t, err)

	assert.False(t, cfg.Watch)
	assert.Equal(t, map[string]interface{}{"name": "x"}, stripped)
}

func Test_ParseWatchConfig_StripsGenericKeys(t *testing.T) {
	raw := map[string]interface{}{"name": "x", "watch": true, "watchinterval": 60, "ttl": 120, "region": "us-east-1"}

	cfg, stripped, err := parseWatchConfig(raw, 1, testErrorMessages())
	require.NoError(t, err)

	assert.True(t, cfg.Watch)
	assert.Equal(t, 60, cfg.WatchInterval)
	assert.Equal(t, 120, cfg.TTL)
	assert.Equal(t, map[string]interface{}{"name": "x", "region": "us-east-1"}, stripped)
	assert.Equal(t, map[string]interface{}{"name": "x", "watch": true, "watchinterval": 60, "ttl": 120, "region": "us-east-1"}, raw, "must not mutate the caller's map")
}

func Test_ParseWatchConfig_DefaultsIntervalWhenWatching(t *testing.T) {
	cfg, _, err := parseWatchConfig(map[string]interface{}{"watch": true}, 1, testErrorMessages())
	require.NoError(t, err)

	assert.Equal(t, defaultWatchInterval, cfg.WatchInterval)
	assert.Equal(t, 0, cfg.TTL)
}

func Test_ParseWatchConfig_IntervalWithoutWatchErrors(t *testing.T) {
	_, _, err := parseWatchConfig(map[string]interface{}{"watchinterval": 60}, 1, testErrorMessages())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret.watch")
	assert.Contains(t, err.Error(), "secret.watchinterval")
}

func Test_ParseWatchConfig_TTLWithoutWatchErrors(t *testing.T) {
	_, _, err := parseWatchConfig(map[string]interface{}{"ttl": 60}, 1, testErrorMessages())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret.ttl")
}

func Test_ParseWatchConfig_IntervalBelowOneErrors(t *testing.T) {
	_, _, err := parseWatchConfig(map[string]interface{}{"watch": true, "watchinterval": 0}, 1, testErrorMessages())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret.watchinterval")
}

func Test_ParseWatchConfig_NegativeTTLErrors(t *testing.T) {
	_, _, err := parseWatchConfig(map[string]interface{}{"watch": true, "ttl": -1}, 1, testErrorMessages())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret.ttl")
}

// A backend reporting no batch capacity has no change detection at all, so asking to watch it is a
// config error rather than a silent no-op
func Test_ParseWatchConfig_WatchOnUnwatchableBackendErrors(t *testing.T) {
	_, _, err := parseWatchConfig(map[string]interface{}{"watch": true}, 0, testErrorMessages())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret.watch")
}

func Test_ParseWatchConfig_NoWatchOnUnwatchableBackendIsFine(t *testing.T) {
	cfg, _, err := parseWatchConfig(map[string]interface{}{"name": "none"}, 0, testErrorMessages())
	require.NoError(t, err)
	assert.False(t, cfg.Watch)
}
