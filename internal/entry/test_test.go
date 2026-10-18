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
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/internal/seed"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

// A minimal valid PNG, since layers with Bounds wrap the provider in a cropper that decodes the image
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

// A pattern layer has no single name, so it expands into its examples rather than testing its bare pattern
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

// Nothing concrete to test, so it's skipped with a warning rather than failing the run
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

// Unaffected by pattern expansion
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

// Lets a pattern layer be tested against a name that isn't one of its examples
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

// Without explicit coordinates the tile must fall inside the layer's bounds and zoom, not the fixed default
func Test_Test_PicksTileWithinLayerBoundsAndZoom(t *testing.T) {
	layer.RegisterProvider(testRecordingRegistration{})

	minZoom := 4
	maxZoom := 6
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:            "bounded_layer",
			LayerMetadata: config.LayerMetadata{MinZoom: &minZoom, MaxZoom: &maxZoom, Bounds: config.BoundsConfig{South: 40, North: 41, West: -74, East: -73}},
			Provider:      map[string]interface{}{"name": "test-recording-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)

	require.GreaterOrEqual(t, lastRecordedTileRequest.Z, minZoom)
	require.LessOrEqual(t, lastRecordedTileRequest.Z, maxZoom)

	bounds := config.BoundsConfig{South: 40, North: 41, West: -74, East: -73}
	zoneRange, err := seed.NewSingleZoomRange(pkg.Bounds{South: bounds.South, North: bounds.North, West: bounds.West, East: bounds.East}, uint(lastRecordedTileRequest.Z)) // #nosec G115 -- Z comes from a zoom range bounded well within int range
	require.NoError(t, err)
	require.GreaterOrEqual(t, lastRecordedTileRequest.X, zoneRange.XMin)
	require.Less(t, lastRecordedTileRequest.X, zoneRange.XMax)
	require.GreaterOrEqual(t, lastRecordedTileRequest.Y, zoneRange.YMin)
	require.Less(t, lastRecordedTileRequest.Y, zoneRange.YMax)
}

// The common unrestricted layer keeps the old fixed default tile
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

// A configured center, including its zoom, wins over the bounds midpoint
func Test_Test_PicksTileFromCenter(t *testing.T) {
	layer.RegisterProvider(testRecordingRegistration{})

	minZoom := 4
	maxZoom := 12
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID: "centered_layer",
			LayerMetadata: config.LayerMetadata{
				MinZoom:          &minZoom,
				MaxZoom:          &maxZoom,
				Bounds:           config.BoundsConfig{South: 30, North: 50, West: -100, East: -60},
				TileJSONMetadata: config.TileJSONMetadata{Center: []float64{-73.5, 40.5, 10}},
			},
			Provider: map[string]interface{}{"name": "test-recording-provider"},
		},
	}

	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Equal(t, pkg.TileRequest{LayerName: "centered_layer", Z: 10, X: 302, Y: 385}, lastRecordedTileRequest)
}

func Test_PickTile_Center(t *testing.T) {
	layer.RegisterProvider(testRecordingRegistration{})

	minZoom := 2
	maxZoom := 6

	tests := []struct {
		name     string
		metadata config.LayerMetadata
		expected pkg.TileRequest
	}{
		{"no zoom uses zoom range midpoint", config.LayerMetadata{MinZoom: &minZoom, MaxZoom: &maxZoom, TileJSONMetadata: config.TileJSONMetadata{Center: []float64{-73.5, 40.5}}}, pkg.TileRequest{Z: 4, X: 4, Y: 6}},
		{"no zoom or range", config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{0.5, 0.5}}}, pkg.TileRequest{Z: 10, X: 513, Y: 510}},
		{"center zoom", config.LayerMetadata{MaxZoom: &maxZoom, TileJSONMetadata: config.TileJSONMetadata{Center: []float64{-73.5, 40.5, 5}}}, pkg.TileRequest{Z: 5, X: 9, Y: 12}},
		{"east edge of the world", config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{180, -85.06, 1}}}, pkg.TileRequest{Z: 1, X: 1, Y: 1}},
		{"lone longitude is ignored", config.LayerMetadata{TileJSONMetadata: config.TileJSONMetadata{Center: []float64{-73.5}}}, pkg.TileRequest{Z: defaultZ, X: defaultX, Y: defaultY}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.LayerConfig{ID: "l", LayerMetadata: tc.metadata, Provider: map[string]interface{}{"name": "test-recording-provider"}}
			l, err := layers.ConstructLayer(context.Background(), cfg, config.ClientConfig{}, nil, config.ErrorMessages{}, nil, nil, nil)
			require.NoError(t, err)

			tc.expected.LayerName = "l"
			require.Equal(t, tc.expected, pickTile(l, "l"))
		})
	}
}

// Explicit coordinates win over derived ones even when the layer has bounds
func Test_Test_ExplicitCoordinatesOverrideAutoPick(t *testing.T) {
	layer.RegisterProvider(testRecordingRegistration{})

	minZoom := 4
	maxZoom := 6
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID:            "bounded_layer",
			LayerMetadata: config.LayerMetadata{MinZoom: &minZoom, MaxZoom: &maxZoom, Bounds: config.BoundsConfig{South: 40, North: 41, West: -74, East: -73}},
			Provider:      map[string]interface{}{"name": "test-recording-provider"},
		},
	}

	// Inside the bounds but distinct from the auto-picked X:9 Y:11, proving the explicit coordinate was used
	var out bytes.Buffer
	errCount, err := Test(&cfg, TestOptions{Z: 5, X: 9, Y: 12, CoordinatesSet: true, NumThread: 1, NoCache: true}, &out)

	require.NoError(t, err)
	require.Equal(t, uint32(0), errCount)
	require.Equal(t, pkg.TileRequest{LayerName: "bounded_layer", Z: 5, X: 9, Y: 12}, lastRecordedTileRequest)
}

// --json without --file replaces the per-tile table with a single JSON summary on stdout
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

// --file alone keeps the table on stdout and writes a plain text summary to the file
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

	content, err := os.ReadFile(filePath) // #nosec G304 -- test-controlled temp path
	require.NoError(t, err)
	require.Contains(t, string(content), "Tested 1 layers, 0 failures")
}

// --file with --json keeps the table on stdout and writes a JSON summary to the file
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

	content, err := os.ReadFile(filePath) // #nosec G304 -- test-controlled temp path
	require.NoError(t, err)

	var summary TestSummary
	require.NoError(t, json.Unmarshal(content, &summary))
	require.Equal(t, 1, summary.Tested)
	require.Equal(t, 0, summary.Failed)
}
