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

package analytics

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ValidateIdentifier(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	valid := []string{
		"events",
		"tile_events",
		"_private",
		"analytics.tile_events",
		"Table123",
		"col$name",
	}

	for _, name := range valid {
		require.NoError(t, validateIdentifier(name, "test", msgs), "%v should be accepted", name)
	}

	// Identifiers are interpolated into SQL rather than bound, so anything that could terminate or
	// extend the statement has to be rejected outright.
	invalid := []string{
		"",
		"1events",
		"tile events",
		"events;DROP TABLE users",
		"events--",
		`events"`,
		"events'",
		"a.b.c",
		"events)",
	}

	for _, name := range invalid {
		assert.Error(t, validateIdentifier(name, "test", msgs), "%v should be rejected", name)
	}
}

func Test_ResolveColumns(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	defaults := map[string]string{
		ColumnTime:  ColumnTime,
		ColumnLayer: ColumnLayer,
	}

	out, err := resolveColumns(defaults, nil, "test", msgs)
	require.NoError(t, err)
	assert.Equal(t, ColumnTime, out[ColumnTime])

	out, err = resolveColumns(defaults, map[string]string{ColumnLayer: "layer_id"}, "test", msgs)
	require.NoError(t, err)
	assert.Equal(t, "layer_id", out[ColumnLayer])
	assert.Equal(t, ColumnTime, out[ColumnTime], "unspecified columns should keep their default")

	// Overriding the defaults must not mutate them; a second module would otherwise inherit them.
	assert.Equal(t, ColumnLayer, defaults[ColumnLayer])

	_, err = resolveColumns(defaults, map[string]string{"nonsense": "x"}, "test", msgs)
	require.Error(t, err, "an override for an unknown logical field is a config error")

	_, err = resolveColumns(defaults, map[string]string{ColumnLayer: "bad name"}, "test", msgs)
	require.Error(t, err, "an override must still be a valid identifier")
}
