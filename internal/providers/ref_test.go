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
	"fmt"
	"testing"

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

// Ref refers to another layer by name; it doesn't own that layer's provider. It must not
// implement Closer, since closing through a ref would either double-close the target layer's
// provider or close it out from under the layer that actually owns it.
func Test_Ref_IsNotACloser(t *testing.T) {
	r := &Ref{}

	_, ok := any(r).(lifecycle.Closer)
	assert.False(t, ok, "Ref must not implement Closer: it references another layer's provider rather than owning one")
}

// layer.validateRefs can't catch a cycle formed via a patterned layer name, so the request-time
// depth counter has to stop it before it overflows the stack.
func Test_Ref_CycleViaPattern_HitsDepthBackstop(t *testing.T) {
	cfg := config.DefaultConfig()

	provider := map[string]any{"name": "ref", "layer": "loop_{n}"}

	cfg.Layers = []config.LayerConfig{
		{ID: "loop", Pattern: "loop_{n}", Provider: provider, Client: &cfg.Client, SkipCache: true},
	}

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()

	_, err = lg.RenderTile(ctx, pkg.TileRequest{LayerName: "loop_1", Z: 1, X: 0, Y: 0})

	require.Error(t, err)
	require.Contains(t, err.Error(), "maximum reference depth")
}

// buildRefChain builds a chain of n ref layers ("chain0" -> "chain1" -> ... -> "chain{n-1}")
// terminating in a static provider, and returns the resulting LayerGroup.
func buildRefChain(t *testing.T, n int) *layer.LayerGroup {
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

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	return lg
}

// The root request is hop 0, so exactly maxRefDepth ref hops must succeed and the limit named in
// the error has to match the number of hops actually attempted. Pins the off-by-one.
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

// Ref rebuilds the context to reset layer pattern placeholders, but the auth restrictions written
// by authentication have to survive that. crop with boundsFromAuth is the control that redacts
// out-of-bounds pixels for a partial-area grant, so dropping allowedArea behind a ref fails open.
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

	lg, err := layer.ConstructLayerGroup(cfg, nil, nil, nil)
	require.NoError(t, err)

	ctx := pkg.BackgroundContext()

	// A grant covering only a sliver of tile 1/0/0 (the northwest quadrant), mimicking what jwt
	// geohash auth installs
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
