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
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func zoomPtr(z int) *int { return &z }

func describedChild() metadataOnlyProvider {
	return metadataOnlyProvider{md: layer.Description{
		DataType:         config.DataTypeRaster,
		MinZoom:          zoomPtr(2),
		MaxZoom:          zoomPtr(10),
		Bounds:           config.BoundsConfig{South: -5, North: 5, West: -5, East: 5},
		TileJSONMetadata: config.TileJSONMetadata{Description: "child", Attribution: "OSM", Center: []float64{1, 2, 3}},
	}}
}

func Test_SingleChildProviders_PassThroughDescription(t *testing.T) {
	child := describedChild()

	wrappers := map[string]layer.Provider{
		"effect":                 Effect{provider: child},
		"transform":              Transform{provider: child},
		"crop without bounds":    Crop{Primary: child},
		"cropmvt without bounds": CropMvt{Primary: child},
		"cropmlt without bounds": CropMlt{Primary: child},
	}

	for name, wrapper := range wrappers {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, layer.DescribeTree(child), layer.DescribeTree(wrapper))
		})
	}
}

func Test_ProvidersHoldingChildren_ImplementParent(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	holders := map[string]bool{}
	methods := map[string]map[string]bool{}

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		require.NoError(t, err)

		ast.Inspect(parsed, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.TypeSpec:
				if st, ok := v.Type.(*ast.StructType); ok && holdsProvider(st) {
					holders[v.Name.Name] = true
				}
			case *ast.FuncDecl:
				if v.Recv != nil && len(v.Recv.List) == 1 {
					recv := receiverName(v.Recv.List[0].Type)
					if methods[recv] == nil {
						methods[recv] = map[string]bool{}
					}
					methods[recv][v.Name.Name] = true
				}
			}
			return true
		})
	}

	require.NotEmpty(t, holders)

	for name := range holders {
		assert.True(t, methods[name]["Children"], "%v holds a layer.Provider so must implement layer.Parent", name)
	}
}

func holdsProvider(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		expr := field.Type
		if arr, ok := expr.(*ast.ArrayType); ok {
			expr = arr.Elt
		}

		if sel, ok := expr.(*ast.SelectorExpr); ok && sel.Sel.Name == "Provider" {
			if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "layer" {
				return true
			}
		}
	}

	return false
}

func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}

	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}

	return ""
}

func Test_NestingProviders_Describe(t *testing.T) {
	primary := describedChild()
	secondary := metadataOnlyProvider{md: layer.Description{
		DataType:         config.DataTypeRaster,
		MinZoom:          zoomPtr(0),
		MaxZoom:          zoomPtr(14),
		Bounds:           config.BoundsConfig{South: 0, North: 20, West: 0, East: 20},
		TileJSONMetadata: config.TileJSONMetadata{Attribution: "Overture"},
	}}
	cropBounds := pkg.Bounds{South: 0, North: 10, West: 0, East: 10}

	union := layer.Description{
		DataType:         config.DataTypeRaster,
		MinZoom:          zoomPtr(0),
		MaxZoom:          zoomPtr(14),
		Bounds:           config.BoundsConfig{South: -5, North: 20, West: -5, East: 20},
		TileJSONMetadata: config.TileJSONMetadata{Description: "child", Attribution: "OSM, Overture", Center: []float64{1, 2, 3}},
	}

	clipped := primary.md
	clipped.Bounds = config.BoundsConfig{South: 0, North: 5, West: 0, East: 5}
	clipped.Center = []float64{1, 2, 3}

	tests := []struct {
		name     string
		provider layer.Provider
		want     layer.Description
	}{
		{
			// Advertising only the primary's zoom range made the layer reject zooms the secondary serves.
			name:     "fallback unions both children",
			provider: Fallback{Primary: primary, Secondary: secondary},
			want:     union,
		},
		{
			name:     "blend unions its children",
			provider: Blend{providers: []layer.Provider{primary, secondary}},
			want:     union,
		},
		{
			name:     "compositemvt unions its children",
			provider: CompositeVector{providers: []layer.Provider{primary, secondary}},
			want:     union,
		},
		{
			name:     "crop with the default secondary clips to its bounds",
			provider: Crop{CropConfig: CropConfig{Bounds: cropBounds}, Primary: primary, Secondary: secondary},
			want:     clipped,
		},
		{
			name:     "crop with a custom secondary unions",
			provider: Crop{CropConfig: CropConfig{Bounds: cropBounds, Secondary: map[string]any{"name": "static"}}, Primary: primary, Secondary: secondary},
			want:     union,
		},
		{
			name:     "crop with bounds from auth passes through",
			provider: Crop{CropConfig: CropConfig{Bounds: cropBounds, BoundsFromAuth: true}, Primary: primary, Secondary: secondary},
			want:     primary.md,
		},
		{
			name:     "cropmvt clips to its bounds",
			provider: CropMvt{CropMvtConfig: CropMvtConfig{Bounds: cropBounds}, Primary: primary},
			want:     clipped,
		},
		{
			name:     "cropmlt clips to its bounds",
			provider: CropMlt{CropMltConfig: CropMltConfig{Bounds: cropBounds}, Primary: primary},
			want:     clipped,
		},
		{
			name:     "cropmvt clip drops a center outside its bounds",
			provider: CropMvt{CropMvtConfig: CropMvtConfig{Bounds: pkg.Bounds{South: 3, North: 10, West: 3, East: 10}}, Primary: primary},
			want: layer.Description{
				DataType: config.DataTypeRaster, MinZoom: zoomPtr(2), MaxZoom: zoomPtr(10),
				Bounds:           config.BoundsConfig{South: 3, North: 5, West: 3, East: 5},
				TileJSONMetadata: config.TileJSONMetadata{Description: "child", Attribution: "OSM"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, layer.DescribeTree(tc.provider))
		})
	}
}

func Test_CropWrapBounds_WrapsBuiltProvider(t *testing.T) {
	child := describedChild()
	bounds := pkg.Bounds{South: 0, North: 10, West: 0, East: 10}

	p, err := CropRegistration{}.WrapBounds(child, bounds, layer.ProviderDeps{})
	require.NoError(t, err)
	crop, ok := p.(*Crop)
	require.True(t, ok)
	assert.Equal(t, child, crop.Primary)
	assert.Equal(t, bounds, crop.Bounds)
	assert.Nil(t, crop.CropConfig.Secondary)

	p, err = CropMvtRegistration{}.WrapBounds(child, bounds, layer.ProviderDeps{})
	require.NoError(t, err)
	cropMvt, ok := p.(*CropMvt)
	require.True(t, ok)
	assert.Equal(t, child, cropMvt.Primary)
	assert.Equal(t, bounds, cropMvt.Bounds)
}

func refFallbackLayerGroup(t *testing.T) *layers.LayerGroup {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Layers = []config.LayerConfig{
		{
			ID: "main",
			Provider: map[string]any{
				"name":      "fallback",
				"primary":   map[string]any{"name": "ref", "layer": "lowres"},
				"secondary": map[string]any{"name": "static", "color": "0F0"},
			},
			Client:    &cfg.Client,
			SkipCache: true,
		},
		{
			ID:            "view",
			Provider:      map[string]any{"name": "ref", "layer": "lowres"},
			LayerMetadata: config.LayerMetadata{Bounds: config.BoundsConfig{South: -10, North: 10, West: -10, East: 10}},
			Client:        &cfg.Client,
			SkipCache:     true,
		},
		{
			ID:            "lowres",
			Provider:      map[string]any{"name": "static", "color": "F00"},
			LayerMetadata: config.LayerMetadata{MaxZoom: zoomPtr(10), TileJSONMetadata: config.TileJSONMetadata{Attribution: "low"}},
			Client:        &cfg.Client,
			SkipCache:     true,
		},
	}

	lg, err := layers.ConstructLayerGroup(context.Background(), cfg, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lg.Close(context.Background()) })

	return lg
}

func Test_LayerGroup_FallbackPastRefTargetZoom_ServesSecondary(t *testing.T) {
	lg := refFallbackLayerGroup(t)

	img, err := lg.RenderTile(pkg.BackgroundContext(), pkg.TileRequest{LayerName: "main", Z: 12, X: 0, Y: 0})
	require.NoError(t, err)
	require.NotNil(t, img)

	main := lg.FindLayer(context.Background(), "main")
	assert.Equal(t, "low", main.Metadata().Advertised.Attribution, "the ref target was built first so its description flows through")
	assert.Nil(t, main.Metadata().Advertised.MaxZoom, "the static secondary has no zoom limit")
}

func Test_LayerGroup_RefWithBounds_InfersDataTypeFromTarget(t *testing.T) {
	lg := refFallbackLayerGroup(t)

	view := lg.FindLayer(context.Background(), "view")
	require.NotNil(t, view)
	assert.Equal(t, config.DataTypeRaster, view.DataType)
	assert.Equal(t, 10, *view.Metadata().Advertised.MaxZoom)

	wrapper, ok := view.Provider.(layers.ProviderWrapper)
	require.True(t, ok)
	assert.Equal(t, "crop", wrapper.Name)
}
