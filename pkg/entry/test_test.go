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
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

// testPNGImage returns a minimal but valid 1x1 PNG, needed by any layer that has Bounds
// configured since that wraps the provider in a crop wrapper which decodes the image.
func testPNGImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}

	return buf.Bytes()
}

type testFixedProvider struct{}

func (testFixedProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (testFixedProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return &pkg.Image{Content: []byte{1, 2, 3}, ContentType: "image/png"}, nil
}

type testFixedRegistration struct{}

func (testFixedRegistration) Name() string                   { return "test-fixed-provider" }
func (testFixedRegistration) InitializeConfig() any          { return struct{}{} }
func (testFixedRegistration) DataType(_ any) config.DataType { return config.DataTypeRaster }
func (testFixedRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return testFixedProvider{}, nil
}

// A pattern layer has no single concrete name, so testing "all layers" (or naming it explicitly)
// must expand it into its configured examples rather than testing its bare id/pattern.
func Test_Test_ExpandsPatternLayerIntoExamples(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:      "pattern_layer",
			Pattern: "my_{name}_{version}",
			Examples: []string{
				"my_foo_v1",
				"my_bar_v2",
			},
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, CoordinatesSet: true, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "my_foo_v1")
	require.Contains(t, out.String(), "my_bar_v2")
	require.NotContains(t, out.String(), "pattern_layer")
}

// A pattern layer without any configured examples has nothing concrete to test, so it's skipped
// with a warning instead of failing the whole run or testing its meaningless bare id/pattern.
func Test_Test_SkipsPatternLayerWithNoExamples(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "pattern_layer",
			Pattern:  "my_{name}_{version}",
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, CoordinatesSet: true, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
}

// A plain id layer keeps being tested under its own name, unaffected by pattern expansion.
func Test_Test_PlainLayerUnaffected(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "plain_layer",
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, CoordinatesSet: true, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "plain_layer")
}

// An explicitly requested name is tested as given, so a pattern layer can be tested against a
// name that isn't one of its examples.
func Test_Test_ExplicitNameUsedAsGiven(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "pattern_layer",
			Pattern:  "my_{name}_{version}",
			Examples: []string{"my_foo_v1"},
			Provider: map[string]interface{}{"name": "test-fixed-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{LayerNames: []string{"my_other_v9"}, Z: 1, X: 0, Y: 0, CoordinatesSet: true, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "my_other_v9")
	require.NotContains(t, out.String(), "my_foo_v1")
}

var lastRecordedTileRequest pkg.TileRequest

type testRecordingProvider struct{}

func (testRecordingProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	pc.AuthBypass = true
	return pc, nil
}

func (testRecordingProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, req pkg.TileRequest) (*pkg.Image, error) {
	lastRecordedTileRequest = req
	return &pkg.Image{Content: testPNGImage(), ContentType: "image/png"}, nil
}

type testRecordingRegistration struct{}

func (testRecordingRegistration) Name() string                   { return "test-recording-provider" }
func (testRecordingRegistration) InitializeConfig() any          { return struct{}{} }
func (testRecordingRegistration) DataType(_ any) config.DataType { return config.DataTypeRaster }
func (testRecordingRegistration) Initialize(_ any, _ layer.ProviderDeps) (layer.Provider, error) {
	return testRecordingProvider{}, nil
}

// Without explicit -z/-x/-y, the picked tile has to fall inside the layer's configured bounds and
// zoom range rather than using the arbitrary fixed default, otherwise the whole point of scoping
// the test to a layer's actual coverage area is lost.
func Test_Test_PicksTileWithinLayerBoundsAndZoom(t *testing.T) {
	layer.RegisterProvider(testRecordingRegistration{})

	minZoom := 4
	maxZoom := 6
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "bounded_layer",
			MinZoom:  &minZoom,
			MaxZoom:  &maxZoom,
			Bounds:   config.BoundsConfig{South: 40, North: 41, West: -74, East: -73},
			Provider: map[string]interface{}{"name": "test-recording-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)

	require.GreaterOrEqual(t, lastRecordedTileRequest.Z, minZoom)
	require.LessOrEqual(t, lastRecordedTileRequest.Z, maxZoom)

	bounds := config.BoundsConfig{South: 40, North: 41, West: -74, East: -73}
	zoneRange, err := (pkg.Bounds{South: bounds.South, North: bounds.North, West: bounds.West, East: bounds.East}).ConstructSingleZoomRange(uint(lastRecordedTileRequest.Z)) //nolint:gosec
	require.NoError(t, err)
	require.GreaterOrEqual(t, lastRecordedTileRequest.X, zoneRange.XMin)
	require.Less(t, lastRecordedTileRequest.X, zoneRange.XMax)
	require.GreaterOrEqual(t, lastRecordedTileRequest.Y, zoneRange.YMin)
	require.Less(t, lastRecordedTileRequest.Y, zoneRange.YMax)
}

// A layer with no bounds/zoom configured keeps using the old fixed default tile, so behavior for
// the common unrestricted layer doesn't change.
func Test_Test_FallsBackToDefaultTileWithoutBoundsOrZoom(t *testing.T) {
	layer.RegisterProvider(testRecordingRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "unrestricted_layer",
			Provider: map[string]interface{}{"name": "test-recording-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Equal(t, pkg.TileRequest{LayerName: "unrestricted_layer", Z: defaultZ, X: defaultX, Y: defaultY}, lastRecordedTileRequest)
}

// Explicit coordinates take priority over bounds/zoom derived ones even when the layer has bounds
// configured.
func Test_Test_ExplicitCoordinatesOverrideAutoPick(t *testing.T) {
	layer.RegisterProvider(testRecordingRegistration{})

	minZoom := 4
	maxZoom := 6
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:       "bounded_layer",
			MinZoom:  &minZoom,
			MaxZoom:  &maxZoom,
			Bounds:   config.BoundsConfig{South: 40, North: 41, West: -74, East: -73},
			Provider: map[string]interface{}{"name": "test-recording-provider"},
		},
	}

	// Z:5 X:9 Y:12 is inside the layer's bounds at z=5 but distinct from the tile auto-pick would
	// choose (X:9 Y:11, the midpoint of the layer's tile range), so this proves the explicit
	// coordinate was used rather than the auto-picked one.
	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 5, X: 9, Y: 12, CoordinatesSet: true, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Equal(t, pkg.TileRequest{LayerName: "bounded_layer", Z: 5, X: 9, Y: 12}, lastRecordedTileRequest)
}

// --json without --file replaces the per-tile text table with a single JSON summary on stdout.
func Test_Test_JSONOutputWithoutFile(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "plain_layer", Provider: map[string]interface{}{"name": "test-fixed-provider"}},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, CoordinatesSet: true, NumThread: 1, NoCache: true, JSON: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.NotContains(t, out.String(), "Thread\tLayer")

	var summary TestSummary
	require.NoError(t, json.Unmarshal(out.Bytes(), &summary))
	require.Equal(t, 1, summary.Tested)
	require.Equal(t, 0, summary.Failed)
	require.Empty(t, summary.Failures)
}

// --file alone keeps streaming the text table to stdout and writes a plain text summary to the
// file.
func Test_Test_FileOutputPlainText(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "plain_layer", Provider: map[string]interface{}{"name": "test-fixed-provider"}},
	}

	filePath := filepath.Join(t.TempDir(), "summary.txt")

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, CoordinatesSet: true, NumThread: 1, NoCache: true, FilePath: filePath}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "plain_layer")

	content, err := os.ReadFile(filePath) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(content), "Tested 1 layers, 0 failures")
}

// --file combined with --json keeps streaming the text table to stdout but writes the JSON
// summary to the file.
func Test_Test_FileOutputJSON(t *testing.T) {
	layer.RegisterProvider(testFixedRegistration{})

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "plain_layer", Provider: map[string]interface{}{"name": "test-fixed-provider"}},
	}

	filePath := filepath.Join(t.TempDir(), "summary.json")

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 1, X: 0, Y: 0, CoordinatesSet: true, NumThread: 1, NoCache: true, JSON: true, FilePath: filePath}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Contains(t, out.String(), "plain_layer")

	content, err := os.ReadFile(filePath) //nolint:gosec
	require.NoError(t, err)

	var summary TestSummary
	require.NoError(t, json.Unmarshal(content, &summary))
	require.Equal(t, 1, summary.Tested)
	require.Equal(t, 0, summary.Failed)
}
