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

package entry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Michad/tilegroxy/pkg/config"
	"gopkg.in/yaml.v3"
)

const (
	DiffFormatText  = "text"
	DiffFormatYAML  = "yaml"
	DiffFormatJSON  = "json"
	DiffFormatTable = "table"
	// The table format as a markdown pipe table
	DiffFormatMarkdown = "markdown"
)

// Impact on a running server started with --hot-reload
const (
	diffImpactReload  = "reload"
	diffImpactRestart = "restart"
)

// Kinds of change within an impact group
const (
	diffKindAdded    = "added"
	diffKindRemoved  = "removed"
	diffKindModified = "modified"
)

type DiffOptions struct {
	Format string
	// ANSI escapes for the text format. Ignored by every other format
	Color bool
}

var reloadableSections = map[string]bool{
	"secret":         true,
	"client":         true,
	"authentication": true,
	"cache":          true,
	"datastores":     true,
	"analytics":      true,
	"layers":         true,
	"health":         true,
}

func DiffConfig(oldCfg, newCfg *config.Config, opts DiffOptions, out io.Writer) (bool, error) {
	if out == nil {
		out = io.Discard
	}

	oldMap, err := configToMap(oldCfg)
	if err != nil {
		return false, err
	}

	newMap, err := configToMap(newCfg)
	if err != nil {
		return false, err
	}

	result := buildDiff(oldMap, newMap)

	if err := renderDiff(result, opts, out); err != nil {
		return false, err
	}

	return len(result) > 0, nil
}

func configToMap(cfg *config.Config) (map[string]any, error) {
	if cfg == nil {
		return map[string]any{}, nil
	}

	// YAML round trip since mapstructure.Decode leaves typed slices like []LayerConfig that the walk can't descend into
	encoded, err := yaml.Marshal(*cfg)
	if err != nil {
		return nil, err
	}

	raw := make(map[string]any)
	if err := yaml.Unmarshal(encoded, &raw); err != nil {
		return nil, err
	}

	lowered, _ := lowerKeys(raw).(map[string]any)

	return lowered, nil
}

func lowerKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(t))
		for k, val := range t {
			res[strings.ToLower(k)] = lowerKeys(val)
		}

		return res
	case []any:
		res := make([]any, len(t))
		for i, val := range t {
			res[i] = lowerKeys(val)
		}

		return res
	case []map[string]any:
		res := make([]any, len(t))
		for i, val := range t {
			res[i] = lowerKeys(val)
		}

		return res
	default:
		return v
	}
}

// impact -> kind -> changed config slice, nested as in a configuration file
type diffResult map[string]map[string]map[string]any

func buildDiff(oldMap, newMap map[string]any) diffResult {
	result := diffResult{}

	for _, section := range sortedKeys(union(oldMap, newMap)) {
		for _, entry := range diffSection(section, oldMap[section], newMap[section]) {
			result.add(entry)
		}
	}

	return result
}

type diffEntry struct {
	impact  string
	kind    string
	section string
	value   any
}

func (d diffResult) add(e diffEntry) {
	kinds, ok := d[e.impact]
	if !ok {
		kinds = map[string]map[string]any{}
		d[e.impact] = kinds
	}

	sections, ok := kinds[e.kind]
	if !ok {
		sections = map[string]any{}
		kinds[e.kind] = sections
	}

	sections[e.section] = mergeInto(sections[e.section], e.value)
}

// Folds several changed keys in one section into a single tree rather than overwriting each other
func mergeInto(existing, addition any) any {
	existingMap, ok1 := existing.(map[string]any)
	additionMap, ok2 := addition.(map[string]any)

	if !ok1 || !ok2 {
		return addition
	}

	for k, v := range additionMap {
		if cur, found := existingMap[k]; found {
			existingMap[k] = mergeInto(cur, v)
			continue
		}

		existingMap[k] = v
	}

	return existingMap
}

// Matched by identifier rather than position so an insert reports one addition instead of shifting everything after it
var keyedSections = map[string]bool{
	"layers":     true,
	"datastores": true,
}

func diffSection(section string, oldVal, newVal any) []diffEntry {
	if keyedSections[section] {
		return diffKeyedList(section, oldVal, newVal)
	}

	changes := diffValue(oldVal, newVal)
	if changes == nil {
		return nil
	}

	return []diffEntry{{impact: sectionImpact(section), kind: diffKindModified, section: section, value: changes}}
}

func sectionImpact(section string) string {
	if reloadableSections[section] {
		return diffImpactReload
	}

	return diffImpactRestart
}

func diffKeyedList(section string, oldVal, newVal any) []diffEntry {
	oldEntries := entriesByID(oldVal)
	newEntries := entriesByID(newVal)
	impact := sectionImpact(section)

	var entries []diffEntry

	for _, id := range sortedKeys(union(oldEntries, newEntries)) {
		oldEntry, inOld := oldEntries[id]
		newEntry, inNew := newEntries[id]

		switch {
		case !inOld:
			entries = append(entries, diffEntry{impact: impact, kind: diffKindAdded, section: section, value: map[string]any{id: wholeValue{pruneEmpty(newEntry)}}})
		case !inNew:
			entries = append(entries, diffEntry{impact: impact, kind: diffKindRemoved, section: section, value: map[string]any{id: wholeValue{pruneEmpty(oldEntry)}}})
		default:
			if changes := diffValue(oldEntry, newEntry); changes != nil {
				entries = append(entries, diffEntry{impact: impact, kind: diffKindModified, section: section, value: map[string]any{id: changes}})
			}
		}
	}

	return entries
}

func entriesByID(val any) map[string]any {
	res := map[string]any{}

	list, ok := val.([]any)
	if !ok {
		return res
	}

	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}

		res[entryKey(entry, i)] = entry
	}

	return res
}

// Entries with neither id nor pattern fall back to their position
func entryKey(entry map[string]any, index int) string {
	if id, ok := entry["id"].(string); ok && id != "" {
		return id
	}

	if pattern, ok := entry["pattern"].(string); ok && pattern != "" {
		return pattern
	}

	return fmt.Sprintf("[%d]", index)
}

// Otherwise a reported layer would list every unset field alongside the few the operator wrote
func pruneEmpty(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}

	res := make(map[string]any, len(m))

	for k, val := range m {
		if isEmpty(val) {
			continue
		}

		res[k] = pruneEmpty(val)
	}

	return res
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case bool:
		return !t
	case map[string]any:
		return len(pruneEmpty(t).(map[string]any)) == 0
	case []any:
		return len(t) == 0
	default:
		return reflect.ValueOf(v).IsZero()
	}
}

// Keeps the surrounding structure, or returns nil when unchanged. A removed key is an explicit null
func diffValue(oldVal, newVal any) any {
	if reflect.DeepEqual(oldVal, newVal) {
		return nil
	}

	if oldMap, ok := oldVal.(map[string]any); ok {
		if newMap, ok := newVal.(map[string]any); ok {
			return diffMap(oldMap, newMap)
		}
	}

	if oldList, ok := oldVal.([]any); ok {
		if newList, ok := newVal.([]any); ok {
			return diffList(oldList, newList)
		}
	}

	return wrapValue(newVal)
}

func diffMap(oldMap, newMap map[string]any) any {
	changes := map[string]any{}

	for _, key := range sortedKeys(union(oldMap, newMap)) {
		if sub := diffValue(oldMap[key], newMap[key]); sub != nil {
			changes[key] = sub
		}
	}

	if len(changes) == 0 {
		return nil
	}

	return changes
}

// Positional so ordered lists like multi tiers report one edited entry, not the whole list. Length changes are reported whole
func diffList(oldList, newList []any) any {
	if len(oldList) != len(newList) {
		return wrapValue(sliceToAny(newList))
	}

	changes := map[string]any{}

	for i := range newList {
		if sub := diffValue(oldList[i], newList[i]); sub != nil {
			changes[fmt.Sprintf("[%d]", i)] = sub
		}
	}

	if len(changes) == 0 {
		return nil
	}

	return changes
}

func sliceToAny(list []any) any {
	if list == nil {
		return nil
	}

	return list
}

// An object added or removed in one piece. Renderers stop here so an added layer reads as one addition
type wholeValue struct {
	value any
}

// nil means "unchanged" to diffValue, so a value that became nil is boxed
type removedValue struct{}

func wrapValue(v any) any {
	if v == nil {
		return removedValue{}
	}

	return v
}

func unwrapValue(v any) any {
	switch t := v.(type) {
	case wholeValue:
		return unwrapValue(t.value)
	case removedValue:
		return nil
	case map[string]any:
		res := make(map[string]any, len(t))
		for k, val := range t {
			res[k] = unwrapValue(val)
		}

		return res
	case []any:
		res := make([]any, len(t))
		for i, val := range t {
			res[i] = unwrapValue(val)
		}

		return res
	default:
		return v
	}
}

func union(maps ...map[string]any) map[string]any {
	res := map[string]any{}

	for _, m := range maps {
		for k := range m {
			res[k] = nil
		}
	}

	return res
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

var diffImpactOrder = []string{diffImpactReload, diffImpactRestart}
var diffKindOrder = []string{diffKindAdded, diffKindRemoved, diffKindModified}

func renderDiff(result diffResult, opts DiffOptions, out io.Writer) error {
	switch opts.Format {
	case DiffFormatJSON:
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")

		return enc.Encode(structuredDiff(result))
	case DiffFormatYAML:
		enc := yaml.NewEncoder(out)
		defer enc.Close()

		return enc.Encode(structuredDiff(result))
	case DiffFormatTable:
		return renderDiffTable(result, out)
	case DiffFormatMarkdown:
		return renderDiffMarkdown(result, out)
	default:
		return renderDiffText(result, opts.Color, out)
	}
}

// Plain maps with internal markers resolved, ready for a serializer
func structuredDiff(result diffResult) map[string]any {
	res := map[string]any{}

	for _, impact := range diffImpactOrder {
		kinds, ok := result[impact]
		if !ok {
			continue
		}

		kindRes := map[string]any{}

		for _, kind := range diffKindOrder {
			if sections, ok := kinds[kind]; ok {
				kindRes[kind] = unwrapValue(sections)
			}
		}

		res[impact] = kindRes
	}

	return res
}

// One row per change, trading nesting for output that sorts and greps by column
func renderDiffTable(result diffResult, out io.Writer) error {
	if len(result) == 0 {
		fmt.Fprintln(out, "No differences")
		return nil
	}

	writer := tabwriter.NewWriter(out, 1, 4, 4, ' ', tabwriter.StripEscape) //nolint:mnd
	fmt.Fprintln(writer, "Can Reload\tChange Kind\tPath\tValue\t")

	for _, row := range tableRows(result) {
		fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t\n", row.canReload, row.kind, row.path, row.value)
	}

	return writer.Flush()
}

type tableRow struct {
	canReload string
	kind      string
	path      string
	value     string
}

// Shared by the table and markdown formats
func tableRows(result diffResult) []tableRow {
	var rows []tableRow

	for _, impact := range diffImpactOrder {
		kinds, ok := result[impact]
		if !ok {
			continue
		}

		canReload := "no"
		if impact == diffImpactReload {
			canReload = "yes"
		}

		for _, kind := range diffKindOrder {
			sections, ok := kinds[kind]
			if !ok {
				continue
			}

			for _, section := range sortedKeys(sections) {
				for _, row := range flattenValue(section, sections[section], formatCell) {
					rows = append(rows, tableRow{canReload: canReload, kind: kind, path: row.path, value: row.value})
				}
			}
		}
	}

	return rows
}

// For pasting into a pull request or job summary
func renderDiffMarkdown(result diffResult, out io.Writer) error {
	if len(result) == 0 {
		fmt.Fprintln(out, "No differences")
		return nil
	}

	fmt.Fprintln(out, "| Can Reload | Change Kind | Path | Value |")
	fmt.Fprintln(out, "| --- | --- | --- | --- |")

	for _, row := range tableRows(result) {
		fmt.Fprintf(out, "| %v | %v | %v | %v |\n", row.canReload, row.kind, escapeMarkdownCell(row.path), escapeMarkdownCell(row.value))
	}

	return nil
}

// Keeps a pipe or backslash from splitting the cell
func escapeMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)

	return strings.ReplaceAll(s, "|", `\|`)
}

type diffRow struct {
	path  string
	value string
}

// A list index is already bracketed, so it attaches without a separator
func joinPath(path, key string) string {
	if strings.HasPrefix(key, "[") {
		return path + key
	}

	return path + "." + key
}

// Joins keys into the dotted config path of each leaf. format differs between line-per-value and column formats
func flattenValue(path string, val any, format func(any) string) []diffRow {
	if whole, ok := val.(wholeValue); ok {
		return []diffRow{{path: path, value: format(whole.value)}}
	}

	nested, ok := val.(map[string]any)
	if !ok || len(nested) == 0 {
		return []diffRow{{path: path, value: format(val)}}
	}

	var rows []diffRow

	for _, key := range sortedKeys(nested) {
		rows = append(rows, flattenValue(joinPath(path, key), nested[key], format)...)
	}

	return rows
}

// A list has no path to split across rows, so it stays inline
func formatCell(val any) string {
	switch t := val.(type) {
	case removedValue, nil:
		return "(removed)"
	case string:
		if t == "" {
			return `""`
		}

		return t
	case []any, map[string]any:
		encoded, err := json.Marshal(unwrapValue(t))
		if err != nil {
			return fmt.Sprintf("%v", t)
		}

		return string(encoded)
	default:
		return fmt.Sprintf("%v", t)
	}
}

const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiBlue  = "\033[34m"
)

var diffKindMarkers = map[string]string{
	diffKindAdded:   "+",
	diffKindRemoved: "-",
}

var diffKindColors = map[string]string{
	diffKindAdded:    ansiGreen,
	diffKindRemoved:  ansiRed,
	diffKindModified: ansiBlue,
}

var diffKindHeadings = map[string]string{
	diffKindAdded:    "Added",
	diffKindRemoved:  "Removed",
	diffKindModified: "Modified",
}

var diffImpactHeadings = map[string]string{
	diffImpactReload:  "Reload-able Changes",
	diffImpactRestart: "Changes Requiring a Restart",
}

// No-op when color is off
type painter bool

func (p painter) paint(attrs, s string) string {
	if !p || attrs == "" {
		return s
	}

	return attrs + s + ansiReset
}

func renderDiffText(result diffResult, color bool, out io.Writer) error {
	paint := painter(color)

	fmt.Fprintf(out, "%v\n%v\n", paint.paint(ansiBold, "Summary"), diffSummary(result))

	for _, impact := range diffImpactOrder {
		kinds, ok := result[impact]
		if !ok {
			continue
		}

		fmt.Fprintf(out, "\n%v\n\n", paint.paint(ansiBold, diffImpactHeadings[impact]))

		writeImpactChanges(out, paint, kinds)
	}

	return nil
}

func diffSummary(result diffResult) string {
	switch {
	case len(result) == 0:
		return "No differences between the two configurations."
	case result[diffImpactRestart] == nil:
		return "Configuration can be entirely reloaded without a restart."
	case result[diffImpactReload] == nil:
		return "Configuration requires a restart to apply."
	default:
		return "Configuration requires a restart to apply all changes."
	}
}

func writeImpactChanges(out io.Writer, paint painter, kinds map[string]map[string]any) {
	first := true
	afterBlock := false

	for _, kind := range diffKindOrder {
		sections, ok := kinds[kind]
		if !ok {
			continue
		}

		for _, section := range sortedKeys(sections) {
			for _, row := range flattenValue(section, sections[section], formatBlock) {
				// Blocks stand apart, while consecutive single line changes read better listed together
				block := strings.HasPrefix(row.value, "\n")
				if !first && (block || afterBlock) {
					fmt.Fprintln(out)
				}

				writeTextChange(out, paint, kind, row)

				first = false
				afterBlock = block
			}
		}
	}
}

func writeTextChange(out io.Writer, paint painter, kind string, row diffRow) {
	color := diffKindColors[kind]

	block, multiline := strings.CutPrefix(row.value, "\n")
	if !multiline {
		fmt.Fprintf(out, "%v: %v\n", diffKindHeadings[kind], paint.paint(color, row.path+": "+row.value))
		return
	}

	fmt.Fprintf(out, "%v %v:\n", diffKindHeadings[kind], row.path)

	prefix := diffKindMarkers[kind]
	if prefix != "" {
		prefix += " "
	}

	for _, line := range strings.Split(block, "\n") {
		fmt.Fprintf(out, "%v\n", paint.paint(color, strings.TrimRight(prefix+line, " ")))
	}
}

func formatBlock(val any) string {
	switch t := val.(type) {
	case []any, map[string]any:
		var buf bytes.Buffer

		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(diffIndentWidth)

		if err := errors.Join(enc.Encode(unwrapValue(t)), enc.Close()); err != nil {
			return formatCell(val)
		}

		return "\n" + strings.TrimRight(buf.String(), "\n")
	default:
		return formatCell(val)
	}
}

const diffIndentWidth = 2
