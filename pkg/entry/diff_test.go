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
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func diffBaseConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{{ID: "main", Provider: map[string]any{"name": "static", "color": "FFF"}}}

	return cfg
}

// Renders the diff as JSON then reads it back, so assertions can navigate the result without
// depending on the text layout
func diffAsMap(t *testing.T, oldCfg, newCfg config.Config) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatJSON}, &buf)
	require.NoError(t, err)

	var res map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &res))

	return res
}

func Test_DiffConfig_IdenticalReportsNoDifferences(t *testing.T) {
	cfg := diffBaseConfig()
	var buf bytes.Buffer

	different, err := DiffConfig(&cfg, &cfg, DiffOptions{}, &buf)

	require.NoError(t, err)
	require.False(t, different)
	require.Contains(t, buf.String(), "No differences")
}

func Test_DiffConfig_NilWriterDoesNotPanic(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999

	require.NotPanics(t, func() {
		different, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{}, nil)
		require.NoError(t, err)
		require.True(t, different)
	})
}

// A missing config is the same as one holding nothing, so comparing against it lists what the other
// side has rather than failing
func Test_DiffConfig_NilConfigIsTreatedAsEmpty(t *testing.T) {
	cfg := diffBaseConfig()
	var buf bytes.Buffer

	different, err := DiffConfig(nil, &cfg, DiffOptions{}, &buf)

	require.NoError(t, err)
	require.True(t, different)
	require.Contains(t, buf.String(), "main")
}

func Test_DiffConfig_ServerChangeNeedsRestart(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999

	res := diffAsMap(t, oldCfg, newCfg)

	require.NotContains(t, res, "reload")
	server := res["restart"].(map[string]any)["modified"].(map[string]any)["server"].(map[string]any)
	require.EqualValues(t, 9999, server["port"])
}

func Test_DiffConfig_CacheChangeCanReload(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Cache = map[string]any{"name": "memory", "maxsize": 100}
	newCfg := diffBaseConfig()
	newCfg.Cache = map[string]any{"name": "memory", "maxsize": 500}

	res := diffAsMap(t, oldCfg, newCfg)

	require.NotContains(t, res, "restart")
	cache := res["reload"].(map[string]any)["modified"].(map[string]any)["cache"].(map[string]any)
	require.EqualValues(t, 500, cache["maxsize"])
	require.NotContains(t, cache, "name")
}

func Test_DiffConfig_AddedLayerIsAnAddition(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers = append(newCfg.Layers, config.LayerConfig{ID: "extra", Provider: map[string]any{"name": "static", "color": "000"}})

	res := diffAsMap(t, oldCfg, newCfg)

	layers := res["reload"].(map[string]any)["added"].(map[string]any)["layers"].(map[string]any)
	require.Contains(t, layers, "extra")
	require.NotContains(t, layers, "main")
}

func Test_DiffConfig_RemovedLayerIsARemoval(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Layers = append(oldCfg.Layers, config.LayerConfig{ID: "extra", Provider: map[string]any{"name": "static", "color": "000"}})
	newCfg := diffBaseConfig()

	res := diffAsMap(t, oldCfg, newCfg)

	layers := res["reload"].(map[string]any)["removed"].(map[string]any)["layers"].(map[string]any)
	require.Contains(t, layers, "extra")
}

// Only the field that changed should appear, nested the way it's written in a config file
func Test_DiffConfig_ModifiedLayerKeepsProviderStructure(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers[0].Provider = map[string]any{"name": "static", "color": "000"}

	res := diffAsMap(t, oldCfg, newCfg)

	layers := res["reload"].(map[string]any)["modified"].(map[string]any)["layers"].(map[string]any)
	provider := layers["main"].(map[string]any)["provider"].(map[string]any)
	require.Equal(t, "000", provider["color"])
	require.NotContains(t, provider, "name")
}

// Reordering the layer list changes nothing about what gets served
func Test_DiffConfig_ReorderedLayersAreNotAChange(t *testing.T) {
	first := config.LayerConfig{ID: "a", Provider: map[string]any{"name": "static", "color": "FFF"}}
	second := config.LayerConfig{ID: "b", Provider: map[string]any{"name": "static", "color": "000"}}

	oldCfg := diffBaseConfig()
	oldCfg.Layers = []config.LayerConfig{first, second}
	newCfg := diffBaseConfig()
	newCfg.Layers = []config.LayerConfig{second, first}

	var buf bytes.Buffer
	different, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{}, &buf)

	require.NoError(t, err)
	require.False(t, different)
}

// A layer identified by a pattern has no id to match on
func Test_DiffConfig_PatternLayerMatchesOnPattern(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Layers = []config.LayerConfig{{Pattern: "p_{v}", Provider: map[string]any{"name": "static", "color": "FFF"}}}
	newCfg := diffBaseConfig()
	newCfg.Layers = []config.LayerConfig{{Pattern: "p_{v}", Provider: map[string]any{"name": "static", "color": "000"}}}

	res := diffAsMap(t, oldCfg, newCfg)

	layers := res["reload"].(map[string]any)["modified"].(map[string]any)["layers"].(map[string]any)
	require.Contains(t, layers, "p_{v}")
}

func Test_DiffConfig_DatastoreMatchesOnID(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Datastores = []map[string]any{{"name": "redis", "id": "r1", "host": "localhost"}}
	newCfg := diffBaseConfig()
	newCfg.Datastores = []map[string]any{{"name": "redis", "id": "r1", "host": "redis.internal"}}

	res := diffAsMap(t, oldCfg, newCfg)

	stores := res["reload"].(map[string]any)["modified"].(map[string]any)["datastores"].(map[string]any)
	require.Equal(t, "redis.internal", stores["r1"].(map[string]any)["host"])
}

// error mixes the two impacts: mode is fixed once handlers are built, messages are read per request
func Test_DiffConfig_ErrorSectionSplitsByKey(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Error.Mode = config.ModeErrorPlainText
	newCfg.Error.Messages.NotAuthorized = "Nope"

	res := diffAsMap(t, oldCfg, newCfg)

	reloaded := res["reload"].(map[string]any)["modified"].(map[string]any)["error"].(map[string]any)
	require.Contains(t, reloaded, "messages")
	require.NotContains(t, reloaded, "mode")

	restarted := res["restart"].(map[string]any)["modified"].(map[string]any)["error"].(map[string]any)
	require.Contains(t, restarted, "mode")
}

// server.health is rebuilt on reload even though the rest of server isn't
func Test_DiffConfig_HealthChangeCanReload(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Health.Checks = []map[string]any{{"name": "layer", "layer": "main"}}

	res := diffAsMap(t, oldCfg, newCfg)

	server := res["reload"].(map[string]any)["modified"].(map[string]any)["server"].(map[string]any)
	require.Contains(t, server, "health")
}

// Several changes within one section have to accumulate rather than overwrite each other
func Test_DiffConfig_MultipleKeysInOneSectionAreMerged(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999
	newCfg.Server.RootPath = "/tiles/"

	res := diffAsMap(t, oldCfg, newCfg)

	server := res["restart"].(map[string]any)["modified"].(map[string]any)["server"].(map[string]any)
	require.EqualValues(t, 9999, server["port"])
	require.Equal(t, "/tiles/", server["rootpath"])
}

// A key present in the old config and gone from the new one is a real change, not an absence
func Test_DiffConfig_RemovedKeyIsReported(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Client.Headers = map[string]string{"X-Foo": "bar"}
	newCfg := diffBaseConfig()
	newCfg.Client.Headers = map[string]string{}

	var buf bytes.Buffer
	different, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{}, &buf)

	require.NoError(t, err)
	require.True(t, different)
	require.Contains(t, buf.String(), "(removed)")
}

func Test_DiffConfig_YAMLFormat(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatYAML}, &buf)
	require.NoError(t, err)

	var res map[string]any
	require.NoError(t, yaml.Unmarshal(buf.Bytes(), &res))
	require.Contains(t, res, "restart")
}

func Test_DiffConfig_TextFormatGroupsByImpactAndKind(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999
	newCfg.Layers = append(newCfg.Layers, config.LayerConfig{ID: "extra", Provider: map[string]any{"name": "static", "color": "000"}})

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatText}, &buf)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, "Reload-able Changes")
	require.Contains(t, out, "Changes Requiring a Restart")
	require.Contains(t, out, "Added layers.extra:")
	require.Contains(t, out, "Modified: server.port: 9999")
}

// The summary leads the report, so it has to name the consequence of the changes below it
func Test_DiffConfig_TextFormatSummary(t *testing.T) {
	base := diffBaseConfig()

	reloadOnly := diffBaseConfig()
	reloadOnly.Layers = append(reloadOnly.Layers, config.LayerConfig{ID: "extra"})

	restartOnly := diffBaseConfig()
	restartOnly.Server.Port = 9999

	both := diffBaseConfig()
	both.Server.Port = 9999
	both.Layers = append(both.Layers, config.LayerConfig{ID: "extra"})

	tests := []struct {
		name    string
		newCfg  config.Config
		summary string
	}{
		{"identical", base, "No differences between the two configurations."},
		{"reload only", reloadOnly, "Configuration can be entirely reloaded without a restart."},
		{"restart only", restartOnly, "Configuration requires a restart to apply."},
		{"both", both, "Configuration requires a restart to apply all changes."},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := DiffConfig(&base, &test.newCfg, DiffOptions{Format: DiffFormatText}, &buf)
			require.NoError(t, err)

			require.Equal(t, "Summary\n"+test.summary+"\n", strings.SplitAfter(buf.String(), test.summary+"\n")[0])
		})
	}
}

// The text format is meant to be read in a terminal, so it colorizes unless asked not to
func Test_DiffConfig_TextFormatColorization(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999

	var colored, plain bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatText, Color: true}, &colored)
	require.NoError(t, err)
	_, err = DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatText}, &plain)
	require.NoError(t, err)

	require.Contains(t, colored.String(), "\033[")
	require.NotContains(t, plain.String(), "\033[")
	require.Equal(t, plain.String(), stripANSI(colored.String()))
}

// Nothing to report still has to respect --no-color
func Test_DiffConfig_TextFormatColorizesNoDifferences(t *testing.T) {
	cfg := diffBaseConfig()

	var colored, plain bytes.Buffer
	_, err := DiffConfig(&cfg, &cfg, DiffOptions{Format: DiffFormatText, Color: true}, &colored)
	require.NoError(t, err)
	_, err = DiffConfig(&cfg, &cfg, DiffOptions{Format: DiffFormatText}, &plain)
	require.NoError(t, err)

	require.Contains(t, colored.String(), "\033[")
	require.Equal(t, "Summary\nNo differences between the two configurations.\n", plain.String())
	require.Equal(t, plain.String(), stripANSI(colored.String()))
}

var ansiPattern = regexp.MustCompile("\033\\[[0-9;]*m")

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// A list value has no structure of its own to walk, so it renders as an indented block
func Test_DiffConfig_TextFormatRendersLists(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Client.StatusCodes = []int{200, 204}
	newCfg := diffBaseConfig()
	newCfg.Client.StatusCodes = []int{200}

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatText}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), "Modified client.statuscodes:\n")
	require.Contains(t, buf.String(), "- 200")
}

func Test_DiffConfig_TableFormatColumns(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999
	newCfg.Layers[0].Provider = map[string]any{"name": "static", "color": "000"}

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatTable}, &buf)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Contains(t, lines[0], "Can Reload")
	require.Contains(t, lines[0], "Change Kind")
	require.Contains(t, lines[0], "Path")
	require.Contains(t, lines[0], "Value")

	// A reloadable layer change and a restart-only server change, each on one row
	require.Contains(t, buf.String(), "yes")
	require.Contains(t, buf.String(), "layers.main.provider.color")
	require.Contains(t, buf.String(), "no")
	require.Contains(t, buf.String(), "server.port")
}

// An added layer is one addition, not one per field it happens to contain
func Test_DiffConfig_TableFormatGroupsAddedLayerIntoOneRow(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers = append(newCfg.Layers, config.LayerConfig{ID: "extra", Provider: map[string]any{"name": "static", "color": "000"}})

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatTable}, &buf)
	require.NoError(t, err)

	// One header row plus the single row for the added layer
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	require.Contains(t, lines[1], "layers.extra")
	require.NotContains(t, buf.String(), "layers.extra.provider.name")
	require.NotContains(t, buf.String(), "map[")

	// The contents are still reported, just within that one row
	require.Contains(t, buf.String(), "static")
}

// Only the field that changed is reported, so a modified layer still breaks down per leaf
func Test_DiffConfig_TableFormatModifiedLayerKeepsLeafRows(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers[0].Provider = map[string]any{"name": "static", "color": "000"}

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatTable}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), "layers.main.provider.color")
}

// A single edit within one tier shouldn't report the whole tiers list as changed
func Test_DiffConfig_ListEntryChangeReportsOnlyThatEntry(t *testing.T) {
	tiers := func(maxSize int) map[string]any {
		return map[string]any{"name": "multi", "tiers": []any{
			map[string]any{"name": "memory", "maxsize": maxSize},
			map[string]any{"name": "disk", "path": "/tmp/tiles"},
		}}
	}

	oldCfg := diffBaseConfig()
	oldCfg.Cache = tiers(100)
	newCfg := diffBaseConfig()
	newCfg.Cache = tiers(500)

	res := diffAsMap(t, oldCfg, newCfg)

	cacheDiff := res["reload"].(map[string]any)["modified"].(map[string]any)["cache"].(map[string]any)
	entry := cacheDiff["tiers"].(map[string]any)["[0]"].(map[string]any)
	require.EqualValues(t, 500, entry["maxsize"])
	require.NotContains(t, entry, "name")
	require.NotContains(t, cacheDiff["tiers"], "[1]")
}

func Test_DiffConfig_TableFormatListEntryPath(t *testing.T) {
	tiers := func(maxSize int) map[string]any {
		return map[string]any{"name": "multi", "tiers": []any{
			map[string]any{"name": "memory", "maxsize": maxSize},
		}}
	}

	oldCfg := diffBaseConfig()
	oldCfg.Cache = tiers(100)
	newCfg := diffBaseConfig()
	newCfg.Cache = tiers(500)

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatTable}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), "cache.tiers[0].maxsize")
}

// Positions stop lining up once a list changes length, so matching them up would misreport
func Test_DiffConfig_ListLengthChangeReportsWholeList(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Cache = map[string]any{"name": "multi", "tiers": []any{
		map[string]any{"name": "memory"},
		map[string]any{"name": "disk"},
	}}
	newCfg := diffBaseConfig()
	newCfg.Cache = map[string]any{"name": "multi", "tiers": []any{
		map[string]any{"name": "memory"},
	}}

	res := diffAsMap(t, oldCfg, newCfg)

	cacheDiff := res["reload"].(map[string]any)["modified"].(map[string]any)["cache"].(map[string]any)
	require.IsType(t, []any{}, cacheDiff["tiers"])
	require.Len(t, cacheDiff["tiers"], 1)
}

// The grouped row still has to carry the layer's contents, not just its name
func Test_DiffConfig_AddedLayerKeepsDetailInStructuredOutput(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers = append(newCfg.Layers, config.LayerConfig{ID: "extra", Provider: map[string]any{"name": "static", "color": "000"}})

	res := diffAsMap(t, oldCfg, newCfg)

	added := res["reload"].(map[string]any)["added"].(map[string]any)["layers"].(map[string]any)
	provider := added["extra"].(map[string]any)["provider"].(map[string]any)
	require.Equal(t, "static", provider["name"])
}

// The text format shows an added layer's contents as a block under its collapsed path, not as a
// change per field, and the block has to line up with the surrounding indentation
func Test_DiffConfig_TextFormatGroupsAddedLayer(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers = append(newCfg.Layers, config.LayerConfig{ID: "extra", Provider: map[string]any{"name": "static", "color": "000"}})

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatText}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), "Added layers.extra:\n")
	require.Contains(t, buf.String(), "+ provider:\n")
	require.Contains(t, buf.String(), "+   name: static")
}

// A layer with nothing set beyond its name prunes down to a scalar, which still has to render
func Test_DiffConfig_TextFormatGroupsEmptyAddedLayer(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers = append(newCfg.Layers, config.LayerConfig{Pattern: "bare_{v}"})

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatText}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), "bare_{v}")
}

// A list has no path of its own to split across rows, so it has to stay on one line
func Test_DiffConfig_TableFormatKeepsListsInline(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Client.StatusCodes = []int{200, 204}
	newCfg := diffBaseConfig()
	newCfg.Client.StatusCodes = []int{200}

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatTable}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), "[200]")
	require.Len(t, strings.Split(strings.TrimSpace(buf.String()), "\n"), 2)
}

func Test_DiffConfig_TableFormatRemovedKey(t *testing.T) {
	oldCfg := diffBaseConfig()
	oldCfg.Client.Headers = map[string]string{"X-Foo": "bar"}
	newCfg := diffBaseConfig()
	newCfg.Client.Headers = map[string]string{}

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatTable}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), "client.headers.x-foo")
	require.Contains(t, buf.String(), "(removed)")
}

func Test_DiffConfig_TableFormatIdentical(t *testing.T) {
	cfg := diffBaseConfig()
	var buf bytes.Buffer

	different, err := DiffConfig(&cfg, &cfg, DiffOptions{Format: DiffFormatTable}, &buf)

	require.NoError(t, err)
	require.False(t, different)
	require.Contains(t, buf.String(), "No differences")
}

func Test_DiffConfig_MarkdownFormatColumns(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Server.Port = 9999
	newCfg.Layers[0].Provider = map[string]any{"name": "static", "color": "000"}

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatMarkdown}, &buf)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Equal(t, "| Can Reload | Change Kind | Path | Value |", lines[0])
	require.Equal(t, "| --- | --- | --- | --- |", lines[1])
	require.Contains(t, buf.String(), "| yes | modified | layers.main.provider.color | 000 |")
	require.Contains(t, buf.String(), "| no | modified | server.port | 9999 |")

	for _, line := range lines[2:] {
		require.Equal(t, 5, strings.Count(line, "|"))
	}
}

func Test_DiffConfig_MarkdownFormatEscapesPipes(t *testing.T) {
	oldCfg := diffBaseConfig()
	newCfg := diffBaseConfig()
	newCfg.Layers[0].Pattern = `a|b\c`

	var buf bytes.Buffer
	_, err := DiffConfig(&oldCfg, &newCfg, DiffOptions{Format: DiffFormatMarkdown}, &buf)
	require.NoError(t, err)

	require.Contains(t, buf.String(), `a\|b\\c`)
}

func Test_DiffConfig_MarkdownFormatIdentical(t *testing.T) {
	cfg := diffBaseConfig()
	var buf bytes.Buffer

	different, err := DiffConfig(&cfg, &cfg, DiffOptions{Format: DiffFormatMarkdown}, &buf)

	require.NoError(t, err)
	require.False(t, different)
	require.Contains(t, buf.String(), "No differences")
}
