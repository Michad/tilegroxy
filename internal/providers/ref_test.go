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
	"net/http"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/caches"
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

// validateRefs can't catch cycles through patterned names, so a repeated name must fail before stack overflow
func Test_Ref_CycleViaPattern_DetectsRepeat(t *testing.T) {
	cfg := config.DefaultConfig()

	provider := map[string]any{"name": "ref", "layer": "loop_{n}"}

	cfg.Layers = []config.LayerConfig{
		{ID: "loop", Pattern: "loop_{n}", Provider: provider, Client: &cfg.Client, SkipCache: true},
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()

	_, err = lg.RenderTile(ctx, pkg.TileRequest{LayerName: "loop_1", Z: 1, X: 0, Y: 0})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle detected: loop_1 -> loop_{n} -> loop_{n}")
}

// A coalesced hop back to an in-flight name would otherwise wait on itself until the deadline
func Test_Ref_CycleViaPlaceholder_Coalesced_FailsFast(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "a", Pattern: "a_{x}", Provider: refProvider("b_{layer.x}"), Client: &cfg.Client, AllowCoalesce: new(true), ParamValidator: map[string]string{"x": "[0-9]+"}},
		{ID: "b", Pattern: "b_{y}", Provider: refProvider("a_{layer.y}"), Client: &cfg.Client, AllowCoalesce: new(true), ParamValidator: map[string]string{"y": "[0-9]+"}},
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, caches.NewSingleCacheRegistry(caches.Noop{}), nil, nil, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	require.NoError(t, err)

	_, err = lg.RenderTile(pkg.NewRequestContext(req), pkg.TileRequest{LayerName: "a_1", Z: 1, X: 0, Y: 0})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle detected: a_1 -> b_1 -> a_1")
	require.NoError(t, ctx.Err(), "cycle should be reported before the deadline")
}

// Names that grow every hop never repeat, so only the depth counter stops them
func Test_Ref_GrowingNames_HitsDepthBackstop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "grow", Pattern: "grow_{n}", Provider: refProvider("grow_{layer.n}x"), Client: &cfg.Client, SkipCache: true},
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)
	require.NoError(t, err)

	_, err = lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "grow_1", Z: 1, X: 0, Y: 0})

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

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)
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

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)
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

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)
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

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)

	require.Error(t, err)
	assert.Nil(t, lg)
}

func Test_Ref_Metadata_UnresolvedIsEmpty(t *testing.T) {
	assert.Equal(t, layer.Description{}, Ref{RefConfig: RefConfig{Layer: "x"}}.Metadata())

	lg := constructPMTilesRefGroup(t, pmtilesLayer("only"))
	assert.Equal(t, layer.Description{}, Ref{RefConfig: RefConfig{Layer: "missing"}, layerGroup: lg}.Metadata())
}

func constructTemplatedRefGroup(t *testing.T, provider map[string]any) *layers.LayerGroup {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "combined", Pattern: "combined_{version}", Provider: provider, Client: &cfg.Client, SkipCache: true},
		{ID: "detailed_v1", Provider: map[string]any{"name": "static", "color": "FF0000"}, Client: &cfg.Client, SkipCache: true},
		{ID: "detailed_v2", Provider: map[string]any{"name": "static", "color": "0000FF"}, Client: &cfg.Client, SkipCache: true},
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)
	require.NoError(t, err)

	return lg
}

func Test_Ref_LayerPlaceholder_ResolvesFromPattern(t *testing.T) {
	lg := constructTemplatedRefGroup(t, map[string]any{
		"name":      "fallback",
		"primary":   refProvider("detailed_{layer.version}"),
		"secondary": map[string]any{"name": "static", "color": "00FF00"},
	})

	for _, version := range []string{"v1", "v2"} {
		img, err := lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "combined_" + version, Z: 1, X: 0, Y: 0})
		require.NoError(t, err)

		direct, err := lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "detailed_" + version, Z: 1, X: 0, Y: 0})
		require.NoError(t, err)

		assert.Equal(t, direct.Content, img.Content, version)
	}
}

func Test_Ref_LayerPlaceholder_InBlendPattern(t *testing.T) {
	lg := constructTemplatedRefGroup(t, map[string]any{
		"name": "blend",
		"mode": "normal",
		"layer": map[string]any{
			"pattern": "{kind}_{layer.version}",
			"values":  []any{map[string]any{"kind": "detailed"}, map[string]any{"kind": "detailed"}},
		},
	})

	img, err := lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "combined_v2", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	direct, err := lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "detailed_v2", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	other, err := lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "combined_v1", Z: 1, X: 0, Y: 0})
	require.NoError(t, err)

	assert.Equal(t, direct.Content, img.Content)
	assert.NotEqual(t, img.Content, other.Content)
}

func Test_Ref_LayerPlaceholder_UnknownFailsStartup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{ID: "combined", Pattern: "combined_{version}", Provider: map[string]any{
			"name":      "fallback",
			"primary":   refProvider("detailed_{layer.typo}"),
			"secondary": map[string]any{"name": "static", "color": "00FF00"},
		}, Client: &cfg.Client, SkipCache: true},
	}

	_, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "layer.provider.primary.layer")
	assert.Contains(t, err.Error(), "{layer.typo}")
}

func Test_Ref_LayerPlaceholder_NoStartupMetadata(t *testing.T) {
	lg := constructPMTilesRefGroup(t,
		config.LayerConfig{ID: "target", Pattern: "target_{n}", Provider: map[string]any{"name": "pmtiles", "file": pmtilesMVTFixture}},
		config.LayerConfig{ID: "outer", Pattern: "outer_{n}", Provider: refProvider("target_{layer.n}")},
	)

	md := lg.FindLayer(context.Background(), "outer_1").Metadata().Advertised
	assert.NotEqual(t, config.DataTypeMVT, md.DataType)
	assert.Empty(t, md.VectorLayers)
}
