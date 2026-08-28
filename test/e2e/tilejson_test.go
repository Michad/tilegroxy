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

//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tileJSONConfig mirrors staticLayerConfig but adds a plain layer and a pattern layer with
// examples, matching the two eligibility cases documented in tilejson.adoc.
const tileJSONConfig = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
  tilejson:
    enabled: true
layers:
  - id: color
    description: A solid color layer
    attribution: "© Example Co"
    provider:
      name: static
      color: "FFFFFF"
  - id: pattern_layer
    pattern: my_{name}_{version}
    paramValidator:
      "*": "^[a-zA-Z0-9]+$"
    examples:
      - my_foo_v1
      - my_bar_v2
    provider:
      name: static
      color: "000000"
`

const tileJSONBaseURLsConfig = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
  tilejson:
    enabled: true
    baseurls:
      - https://tiles-a.example.com
      - https://tiles-b.example.com/maps
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

const tileJSONDisabledConfig = `
server:
  port: {{.Port}}
  production: false
  drainDelay: 0
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

type tileJSONDocument struct {
	TileJSON string    `json:"tilejson"`
	Name     string    `json:"name"`
	Tiles    []string  `json:"tiles"`
	MinZoom  int       `json:"minzoom"`
	MaxZoom  int       `json:"maxzoom"`
	Bounds   []float64 `json:"bounds"`

	Description string `json:"description,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

type tileJSONIndexEntry struct {
	Name     string `json:"name"`
	TileJSON string `json:"tilejson"`
}

// When TileJSON is disabled the paths it would otherwise serve fall through to the default
// unrouted-path handler, which redirects to the documentation site rather than the tilejson
// document a caller would get when it's enabled.
func Test_TileJSON_DisabledByDefault(t *testing.T) {
	inst := Start(t, Config{Raw: tileJSONDisabledConfig})

	inst.GetNoRedirect("/tilejson.json").
		ExpectStatus(http.StatusTemporaryRedirect).
		ExpectHeader("Location", "/docs")
	inst.GetNoRedirect("/tiles/color.json").
		ExpectStatus(http.StatusTemporaryRedirect).
		ExpectHeader("Location", "/docs")
}

func Test_TileJSON_IndexListsEligibleLayers(t *testing.T) {
	inst := Start(t, Config{Raw: tileJSONConfig})

	resp := inst.Get("/tilejson.json").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "application/json")

	var index []tileJSONIndexEntry
	require.NoError(t, json.Unmarshal(resp.Body, &index))

	names := make([]string, 0, len(index))
	for _, entry := range index {
		names = append(names, entry.Name)
		assert.Contains(t, entry.TileJSON, entry.Name+".json")
	}

	// The plain layer is eligible automatically; the pattern layer only through its examples.
	assert.Contains(t, names, "color")
	assert.Contains(t, names, "my_foo_v1")
	assert.Contains(t, names, "my_bar_v2")
	assert.NotContains(t, names, "pattern_layer")
}

func Test_TileJSON_PlainLayerDocumentMatchesDocumentedFields(t *testing.T) {
	inst := Start(t, Config{Raw: tileJSONConfig})

	resp := inst.Get("/tiles/color.json").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "application/json")

	var doc tileJSONDocument
	require.NoError(t, json.Unmarshal(resp.Body, &doc))

	assert.Equal(t, "3.0.0", doc.TileJSON)
	assert.Equal(t, "color", doc.Name)
	require.Len(t, doc.Tiles, 1)
	assert.Contains(t, doc.Tiles[0], "/tiles/color/{z}/{x}/{y}")
	assert.Equal(t, 0, doc.MinZoom)
	assert.Equal(t, 21, doc.MaxZoom)
	require.Len(t, doc.Bounds, 4)
	assert.InDelta(t, -180, doc.Bounds[0], 0.0001)
	assert.InDelta(t, -85.05112878, doc.Bounds[1], 0.0001)
	assert.InDelta(t, 180, doc.Bounds[2], 0.0001)
	assert.InDelta(t, 85.05112878, doc.Bounds[3], 0.0001)
	assert.Equal(t, "A solid color layer", doc.Description)
	assert.Equal(t, "© Example Co", doc.Attribution)
}

func Test_TileJSON_PatternLayerExampleServedByName(t *testing.T) {
	inst := Start(t, Config{Raw: tileJSONConfig})

	resp := inst.Get("/tiles/my_foo_v1.json").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "application/json")

	var doc tileJSONDocument
	require.NoError(t, json.Unmarshal(resp.Body, &doc))

	assert.Equal(t, "3.0.0", doc.TileJSON)
	assert.Equal(t, "my_foo_v1", doc.Name)
	require.Len(t, doc.Tiles, 1)
	assert.Contains(t, doc.Tiles[0], "/tiles/my_foo_v1/{z}/{x}/{y}")
	// Examples have no description/attribution set on the underlying pattern layer.
	assert.Empty(t, doc.Description)
	assert.Empty(t, doc.Attribution)
}

// An unknown layer name isn't distinguishable from one that's merely unauthorized for the
// caller, so it's rejected the same way a tile request for it would be.
func Test_TileJSON_UnknownLayerIsUnauthorized(t *testing.T) {
	inst := Start(t, Config{Raw: tileJSONConfig})

	inst.Get("/tiles/nosuchlayer.json").ExpectStatus(http.StatusUnauthorized)
}

func Test_TileJSON_BaseURLsProducesOneEntryPerConfiguredURL(t *testing.T) {
	inst := Start(t, Config{Raw: tileJSONBaseURLsConfig})

	resp := inst.Get("/tiles/color.json").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "application/json")

	var doc tileJSONDocument
	require.NoError(t, json.Unmarshal(resp.Body, &doc))

	require.Len(t, doc.Tiles, 2)
	assert.Equal(t, "https://tiles-a.example.com/tiles/color/{z}/{x}/{y}", doc.Tiles[0])
	assert.Equal(t, "https://tiles-b.example.com/maps/tiles/color/{z}/{x}/{y}", doc.Tiles[1])
}
