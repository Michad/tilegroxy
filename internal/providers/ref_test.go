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

package providers

import (
	"context"
	"fmt"
	"testing"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_DataType_Ref(t *testing.T) {
	assert.Equal(t, config.DataTypeUnknown, RefRegistration{}.DataType(RefConfig{}))
}

// Ref doesn't own the target's provider, so closing through it would double-close or close it under its owner
func Test_Ref_IsNotACloser(t *testing.T) {
	r := &Ref{}

	_, ok := any(r).(lifecycle.Closer)
	assert.False(t, ok, "Ref must not implement Closer: it references another layer's provider rather than owning one")
}

// validateRefs can't catch cycles through patterned names, so the depth counter must stop them before stack overflow
func Test_Ref_CycleViaPattern_HitsDepthBackstop(t *testing.T) {
	cfg := config.DefaultConfig()

	provider := map[string]any{"name": "ref", "layer": "loop_{n}"}

	cfg.Layers = []config.LayerConfig{
		{ID: "loop", Pattern: "loop_{n}", Provider: provider, Client: &cfg.Client, SkipCache: true},
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil)
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()

	_, err = lg.RenderTile(ctx, pkg.TileRequest{LayerName: "loop_1", Z: 1, X: 0, Y: 0})

	require.Error(t, err)
	require.Contains(t, err.Error(), "maximum reference depth")
}

// chain0 -> chain1 -> ... -> chain{n-1}, ending in a static provider
func buildRefChain(t *testing.T, n int) *layers.LayerGroup {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Layers = make([]config.LayerConfig, 0, n+1)

	for i := range n {
		next := fmt.Sprintf("chain%d", i+1)
		if i == n-1 {
			next = "chainEnd"
		}
		provider := map[string]any{"name": "ref", "layer": next}
		cfg.Layers = append(cfg.Layers, config.LayerConfig{ID: fmt.Sprintf("chain%d", i), Provider: provider, Client: &cfg.Client, SkipCache: true})
	}

	cfg.Layers = append(cfg.Layers, config.LayerConfig{ID: "chainEnd", Provider: map[string]any{"name": "static", "color": "FFF0"}, Client: &cfg.Client, SkipCache: true})

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil)
	require.NoError(t, err)

	return lg
}

// Pins the off-by-one: the root is hop 0, so exactly maxRefDepth hops succeed and the error names that limit
func Test_Ref_DepthLimit_MatchesDocumentedHopCount(t *testing.T) {
	lg := buildRefChain(t, maxRefDepth)

	ctx := pkg.BackgroundContext()
	_, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "chain0", Z: 1, X: 0, Y: 0})
	require.NoError(t, err, "exactly maxRefDepth ref hops should be allowed")

	lgTooLong := buildRefChain(t, maxRefDepth+1)
	_, err = lgTooLong.RenderTile(ctx, pkg.TileRequest{LayerName: "chain0", Z: 1, X: 0, Y: 0})
	require.Error(t, err, "maxRefDepth+1 ref hops should be rejected")
	require.Contains(t, err.Error(), fmt.Sprintf("maximum reference depth (%d) exceeded", maxRefDepth))
}

// Ref rebuilds the context, but dropping allowedArea would let crop with boundsFromAuth fail open
func Test_Ref_PropagatesAuthRestrictions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "outer", Provider: map[string]any{"name": "ref", "layer": "inner"}, Client: &cfg.Client, SkipCache: true},
		{ID: "inner", Client: &cfg.Client, SkipCache: true, Provider: map[string]any{
			"name":           "crop",
			"boundsFromAuth": true,
			"primary":        map[string]any{"name": "static", "color": "FF0000FF"},
		}},
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil)
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()

	// A sliver of tile 1/0/0, mimicking what jwt geohash auth installs
	area, ok := pkg.AllowedAreaFromContext(ctx)
	require.True(t, ok)
	*area = pkg.Bounds{South: 10, North: 20, West: -20, East: -10}
	partial, ok := pkg.LimitAreaPartialFromContext(ctx)
	require.True(t, ok)
	*partial = true

	img, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "outer", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)
	require.NotNil(t, img)

	direct, err := lg.RenderTile(ctx, pkg.TileRequest{LayerName: "inner", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	assert.Equal(t, direct.Content, img.Content, "tile behind a ref must be cropped to the same auth bounds as one requested directly")
}

func constructPMTilesRefGroup(t *testing.T, layerConfigs ...config.LayerConfig) *layers.LayerGroup {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Layers = layerConfigs

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lg.Close(context.Background()) })

	return lg
}

func pmtilesLayer(id string) config.LayerConfig {
	return config.LayerConfig{ID: id, Provider: map[string]any{"name": "pmtiles", "file": pmtilesMVTFixture}}
}

func refProvider(target string) map[string]any {
	return map[string]any{"name": "ref", "layer": target}
}

func Test_Ref_Metadata_ResolvesTargetsDefinedBeforeAndAfter(t *testing.T) {
	lg := constructPMTilesRefGroup(t,
		pmtilesLayer("before"),
		config.LayerConfig{ID: "composite", Provider: map[string]any{
			"name":      "compositemvt",
			"providers": []any{refProvider("before"), refProvider("after")},
		}},
		pmtilesLayer("after"),
	)

	target := lg.FindLayer(context.Background(), "before")
	require.NotEmpty(t, target.Metadata().Advertised.VectorLayers)

	composite := lg.FindLayer(context.Background(), "composite")
	doc := composite.BuildTileJSON("composite", nil, nil)

	assert.Equal(t, config.DataTypeMVT, composite.DataType)
	assert.Equal(t, target.Metadata().Advertised.VectorLayers, doc.VectorLayers)
	assert.Equal(t, target.Metadata().Advertised.Attribution, doc.Attribution)
	// The archive's zoom range is advertised but not enforced since the layer sets no limits of its own
	require.NoError(t, composite.CheckZoomBounds(pkg.TileRequest{Z: 2}))
}

func Test_Ref_Metadata_FollowsChainAndTargetConfig(t *testing.T) {
	target := pmtilesLayer("target")
	target.Attribution = "configured"

	lg := constructPMTilesRefGroup(t,
		config.LayerConfig{ID: "outer", Provider: refProvider("middle")},
		config.LayerConfig{ID: "middle", Provider: refProvider("target")},
		target,
	)

	md := lg.FindLayer(context.Background(), "outer").Metadata().Advertised

	assert.Equal(t, config.DataTypeMVT, md.DataType)
	assert.Equal(t, "configured", md.Attribution)
	assert.NotEmpty(t, md.VectorLayers)
}

func Test_Ref_Metadata_ContradictingDataTypeFails(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "outer", LayerMetadata: config.LayerMetadata{DataType: config.DataTypeRaster}, Provider: refProvider("target")},
		pmtilesLayer("target"),
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil)

	require.Error(t, err)
	assert.Nil(t, lg)
}

func Test_Ref_Metadata_UnresolvedIsEmpty(t *testing.T) {
	assert.Equal(t, layer.Description{}, Ref{RefConfig: RefConfig{Layer: "x"}}.Metadata())

	lg := constructPMTilesRefGroup(t, pmtilesLayer("only"))
	assert.Equal(t, layer.Description{}, Ref{RefConfig: RefConfig{Layer: "missing"}, layerGroup: lg}.Metadata())
}
