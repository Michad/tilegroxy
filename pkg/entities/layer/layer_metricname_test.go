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

package layer

import (
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

func Test_SanitizeMetricName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary id unchanged", "osm", "osm"},
		{"space replaced", "my layer", "my_layer"},
		{"dot replaced", "my.layer", "my_layer"},
		{"slash replaced", "my/layer", "my_layer"},
		{"non-ASCII replaced", "层", "_"},
		{"mixed non-ASCII and ASCII", "my层layer", "my_layer"},
		{"leading digit preserved as-is (only char class matters)", "123layer", "123layer"},
		{"hyphen and underscore preserved", "my-layer_2", "my-layer_2"},
		{"multiple invalid chars each replaced", "a b/c.d", "a_b_c_d"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeMetricName(c.in)
			require.Equal(t, c.want, got)
		})
	}
}

func Test_SanitizeMetricName_TruncatesLongIDs(t *testing.T) {
	var longIDSb strings.Builder
	for range 300 {
		longIDSb.WriteString("a")
	}
	longID := longIDSb.String()

	got := sanitizeMetricName(longID)
	require.LessOrEqual(t, len(got), maxSanitizedMetricNameLen)
	require.Len(t, got, maxSanitizedMetricNameLen)
}

// A layer ID with a space or certain non-ASCII characters used to be embedded directly into the
// OTEL metric *name* (e.g. "tilegroxy.tiles.layer.my layer.request"), which isn't a valid
// instrument name and made Int64Counter construction fail - fatal at startup. sanitizeMetricName
// now replaces such characters with '_' before they're embedded in the metric name, so
// construction succeeds regardless of what characters are in the layer ID.
func Test_ConstructLayer_LayerIDWithSpaceDoesNotFailConstruction(t *testing.T) {
	RegisterProvider(docExampleSampleRegistration{})

	rawConfig := config.LayerConfig{
		ID:       "my layer",
		Provider: map[string]any{"name": "doc-example-sample"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
}

func Test_ConstructLayer_LayerIDWithNonASCIIDoesNotFailConstruction(t *testing.T) {
	RegisterProvider(docExampleSampleRegistration{})

	rawConfig := config.LayerConfig{
		ID:       "层",
		Provider: map[string]any{"name": "doc-example-sample"},
	}

	l, err := ConstructLayer(rawConfig, config.ClientConfig{}, config.ErrorMessages{}, nil, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, l)
}
