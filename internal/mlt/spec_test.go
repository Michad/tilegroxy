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

//go:build mltspec

package mlt

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const specRepo = "https://github.com/maplibre/maplibre-tile-spec.git"

var specDir string

// Set MLT_SPEC_DIR to use an existing checkout, or MLT_SPEC_REF to pick the branch, tag, or commit to fetch.
func TestMain(m *testing.M) {
	specDir = os.Getenv("MLT_SPEC_DIR")

	if specDir == "" {
		tmp, err := os.MkdirTemp("", "maplibre-tile-spec")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		specDir = tmp

		if err := checkoutSpec(tmp); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.RemoveAll(tmp)
			os.Exit(1)
		}

		code := m.Run()
		os.RemoveAll(tmp)
		os.Exit(code)
	}

	os.Exit(m.Run())
}

func checkoutSpec(dir string) error {
	ref := os.Getenv("MLT_SPEC_REF")
	if ref == "" {
		ref = "main"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	steps := [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", specRepo},
		{"sparse-checkout", "set", "test/synthetic", "test/fixtures/fastpfor"},
		{"fetch", "--quiet", "--depth", "1", "--filter", "blob:none", "origin", ref},
		{"checkout", "--quiet", "FETCH_HEAD"},
	}

	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- ref comes from the developer running the test
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %w\n%s", strings.Join(args, " "), err, out)
		}
	}

	return nil
}

// Only the 0x01 layer format is supported, so later format folders like 0x02 are left out.
func Test_SpecSynthetic(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(specDir, "test", "synthetic", "0x01*"))
	require.NoError(t, err)
	require.NotEmpty(t, dirs)

	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			runConformance(t, dir)
		})
	}
}

// Covers FastPFOR blocks with exceptions, which no tile exercises.
func Test_SpecFastPFOR(t *testing.T) {
	dir := filepath.Join(specDir, "test", "fixtures", "fastpfor")

	files, err := filepath.Glob(filepath.Join(dir, "*_encoded.bin"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), "_encoded.bin")

		t.Run(name, func(t *testing.T) {
			encoded, err := os.ReadFile(f)
			require.NoError(t, err)

			decoded, err := os.ReadFile(filepath.Join(dir, name+"_decoded.bin"))
			require.NoError(t, err)

			want := make([]uint64, len(decoded)/word32Bytes)
			for j := range want {
				want[j] = uint64(binary.BigEndian.Uint32(decoded[j*word32Bytes:]))
			}

			got, err := decodeFastPFOR(encoded, uint32(len(want)))
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func runConformance(t *testing.T, dir string) {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.mlt"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	out := os.Getenv("MLT_REENCODE_OUT")

	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".mlt")

		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(f)
			require.NoError(t, err)

			layers, skipped, err := Decode(data)
			require.NoError(t, err)
			assert.Zero(t, skipped)

			reencoded, err := Encode(layers)
			require.NoError(t, err)

			again, _, err := Decode(reencoded)
			require.NoError(t, err)

			if out != "" {
				require.NoError(t, os.WriteFile(filepath.Join(out, name+".mlt"), reencoded, 0600))
			}

			// Tiles without an expected output only have to survive being encoded again.
			expected, err := os.ReadFile(strings.TrimSuffix(f, ".mlt") + ".json")
			if os.IsNotExist(err) {
				assert.Equal(t, layers, again)
				return
			}
			require.NoError(t, err)

			assertMatchesGeoJSON(t, expected, layers)
			assertMatchesGeoJSON(t, expected, again)

			if out != "" {
				require.NoError(t, os.WriteFile(filepath.Join(out, name+".json"), expected, 0600))
			}
		})
	}
}

type expectedFeature struct {
	ID         *json.Number
	Properties map[string]any
	Geometry   struct {
		Type        string
		Coordinates any
	}
}

type decodedFeature struct {
	id         *uint64
	properties map[string]any
	geometry   orb.Geometry
}

func flatten(layers []Layer) []decodedFeature {
	var out []decodedFeature

	for _, l := range layers {
		for i, g := range l.Geometries {
			f := decodedFeature{geometry: g, properties: map[string]any{"_extent": l.Extent, "_layer": l.Name}}

			if l.ID != nil && (!l.ID.Nullable || l.ID.Present[i]) {
				f.id = &l.ID.Values[i]
			}

			for _, c := range l.Columns {
				addProperties(f.properties, "", c, i)
			}

			out = append(out, f)
		}
	}

	return out
}

func addProperties(props map[string]any, prefix string, c Column, i int) {
	for _, child := range c.Children {
		addProperties(props, prefix+c.Name, child, i)
	}

	if c.Values == nil || (c.Nullable && !c.Present[i]) {
		return
	}

	switch v := c.Values.(type) {
	case []bool:
		props[prefix+c.Name] = v[i]
	case []int32:
		props[prefix+c.Name] = v[i]
	case []uint32:
		props[prefix+c.Name] = v[i]
	case []int64:
		props[prefix+c.Name] = v[i]
	case []uint64:
		props[prefix+c.Name] = v[i]
	case []float32:
		props[prefix+c.Name] = v[i]
	case []float64:
		props[prefix+c.Name] = v[i]
	case []string:
		props[prefix+c.Name] = v[i]
	}
}

func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	require.NoError(t, dec.Decode(target))
}

func assertMatchesGeoJSON(t *testing.T, expectedJSON []byte, layers []Layer) {
	t.Helper()

	var expected struct{ Features []expectedFeature }
	decodeJSON(t, expectedJSON, &expected)

	got := flatten(layers)
	require.Len(t, got, len(expected.Features))

	for i, exp := range expected.Features {
		g := got[i]

		if exp.ID == nil {
			assert.Nil(t, g.id, "feature %v id", i)
		} else if assert.NotNil(t, g.id, "feature %v id", i) {
			assert.Equal(t, exp.ID.String(), strconv.FormatUint(*g.id, 10), "feature %v id", i)
		}

		assert.Len(t, g.properties, len(exp.Properties), "feature %v properties %v", i, g.properties)

		for k, v := range exp.Properties {
			assertValue(t, v, g.properties[k], fmt.Sprintf("feature %v property %v", i, k))
		}

		gotJSON, err := json.Marshal(geojson.NewGeometry(g.geometry))
		require.NoError(t, err)

		var gotGeometry struct {
			Type        string
			Coordinates any
		}
		decodeJSON(t, gotJSON, &gotGeometry)

		assert.Equal(t, exp.Geometry.Type, gotGeometry.Type, "feature %v geometry type", i)
		assert.Equal(t, exp.Geometry.Coordinates, gotGeometry.Coordinates, "feature %v coordinates", i)
	}
}

func assertValue(t *testing.T, expected any, got any, msg string) {
	t.Helper()

	switch g := got.(type) {
	case float32:
		assertFloat(t, expected, float64(g), 32, msg)
	case float64:
		assertFloat(t, expected, g, 64, msg)
	default:
		if n, ok := expected.(json.Number); ok {
			assert.Equal(t, n.String(), fmt.Sprint(got), msg)
		} else {
			assert.Equal(t, expected, got, msg)
		}
	}
}

// Special values are written as strings like f32::NAN.
func assertFloat(t *testing.T, expected any, got float64, bits int, msg string) {
	t.Helper()

	if s, ok := expected.(string); ok {
		_, special, _ := strings.Cut(s, "::")

		switch special {
		case "NAN":
			assert.True(t, math.IsNaN(got), msg)
		case "INFINITY":
			assert.True(t, math.IsInf(got, 1), msg)
		case "NEG_INFINITY":
			assert.True(t, math.IsInf(got, -1), msg)
		default:
			assert.Fail(t, "unexpected float "+s, msg)
		}

		return
	}

	n, ok := expected.(json.Number)
	require.True(t, ok, msg)

	want, err := strconv.ParseFloat(n.String(), bits)
	require.NoError(t, err, msg)

	if bits == 32 {
		assert.Equal(t, math.Float32bits(float32(want)), math.Float32bits(float32(got)), msg)
	} else {
		assert.Equal(t, math.Float64bits(want), math.Float64bits(got), msg)
	}
}
