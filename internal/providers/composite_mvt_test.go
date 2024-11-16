package providers

import (
	"testing"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Composite_ExecuteStatic(t *testing.T) {
	provConfig := map[string]interface{}{
		"name":  "static",
		"image": "embedded:box.mvt",
	}

	c, err := CompositeMVTRegistration{}.Initialize(CompositeMVTConfig{Providers: []map[string]interface{}{provConfig, provConfig}}, testClientConfig, testErrMessages, nil, nil)

	assert.NotNil(t, c)
	require.NoError(t, err)

	pc, err := c.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	assert.NotNil(t, pc)
	require.NoError(t, err)

	img, err := c.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	assert.NotNil(t, img)
	require.NoError(t, err)

	imgExp, err := images.GetStaticImage("embedded:box.mvt")
	require.NoError(t, err)

	assert.Equal(t, len(*imgExp) * 2, len(img.Content))
}
