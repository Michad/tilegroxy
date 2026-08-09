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

package providers

import (
	"fmt"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/require"
)

// A ref cycle formed via a patterned layer name can't be caught by the static startup
// validation in layer.validateRefs. This proves the request-time depth counter backstop
// stops it instead of stack-overflowing the process.
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

// The root request is hop 0, so a chain of exactly maxRefDepth ref hops must succeed - anything
// beyond that is rejected, and the error message's stated limit should match the actual number
// of hops attempted when it trips. This pins the off-by-one: previously the check ran before the
// child depth was incremented, so the (maxRefDepth+1)-th hop tripped it while the message still
// claimed only maxRefDepth were made.
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
