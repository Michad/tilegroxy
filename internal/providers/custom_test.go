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
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The minimum script needed to initialize, so close-specific tests don't restate it
const existingRequiredFuncs = `
func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, cientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
)  (tilegroxy.ProviderContext, error) {
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages ) (*tilegroxy.Image, error ) {
	return &tilegroxy.Image{Content:[]byte{0x01,0x02}}, nil
}
`

const customScriptHeader = `
package custom

import (
	"context"
	"os"

	"tilegroxy/tilegroxy"
)
`

func buildCustomProviderFromScript(t *testing.T, body string) *Custom {
	t.Helper()

	c, err := CustomRegistration{}.Initialize(CustomConfig{Script: customScriptHeader + body}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	require.NoError(t, err)
	require.NotNil(t, c)

	return c.(*Custom)
}

func Test_DataType_Custom(t *testing.T) {
	assert.Equal(t, config.DataTypeUnknown, CustomRegistration{}.DataType(CustomConfig{}))
}

func Test_CustomValidate(t *testing.T) {
	c, err := CustomRegistration{}.Initialize(CustomConfig{}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, c)
	require.Error(t, err)

	c, err = CustomRegistration{}.Initialize(CustomConfig{Script: "package custom"}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, c)
	require.Error(t, err)
}

func Test_CustomExecute(t *testing.T) {
	c, err := CustomRegistration{}.Initialize(CustomConfig{Script: `
package custom

import (
	"math/rand"
	"strconv"
	"strings"

	"tilegroxy/tilegroxy"
)
func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, cientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
)  (tilegroxy.ProviderContext, error) {
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages ) (*tilegroxy.Image, error ) {
	return &tilegroxy.Image{Content:[]byte{0x01,0x02}}, nil
}
	`}, layer.ProviderDeps{ErrorMessages: testErrMessages})

	if err != nil {
		fmt.Println(err)
	}

	assert.NotNil(t, c)
	require.NoError(t, err)

	pc, err := c.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{})
	require.NoError(t, err)
	assert.NotNil(t, pc)
	assert.True(t, pc.AuthBypass)

	img, err := c.GenerateTile(pkg.BackgroundContext(), pc, pkg.TileRequest{LayerName: "l", Z: 3, X: 1, Y: 2})
	require.NoError(t, err)
	assert.NotNil(t, img)
	assert.Equal(t, []byte{0x01, 0x02}, img.Content)

}

func Test_CustomProviderCloseInvokesScript(t *testing.T) {
	out := filepath.Join(t.TempDir(), "closed.txt")

	script := `
func close(ctx context.Context) error {
	return os.WriteFile("` + out + `", []byte("closed"), 0600)
}
` + existingRequiredFuncs

	p := buildCustomProviderFromScript(t, script)

	require.NoError(t, p.Close(pkg.BackgroundContext()))

	content, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "closed", string(content))
}

func Test_CustomProviderCloseOptionalWhenAbsent(t *testing.T) {
	// Scripts written before close existed must keep working
	p := buildCustomProviderFromScript(t, existingRequiredFuncs)

	require.NoError(t, p.Close(pkg.BackgroundContext()))
}

func Test_CustomProviderCloseWrongSignatureFailsAtInitialize(t *testing.T) {
	script := customScriptHeader + `
func close() {}
` + existingRequiredFuncs

	c, err := CustomRegistration{}.Initialize(CustomConfig{Script: script}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, c)
	require.Error(t, err)
}

func Test_CustomProviderPreAuthWrongSignatureFailsAtInitialize(t *testing.T) {
	script := customScriptHeader + `
func preAuth() {}

func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages ) (*tilegroxy.Image, error ) {
	return &tilegroxy.Image{Content:[]byte{0x01,0x02}}, nil
}
`

	c, err := CustomRegistration{}.Initialize(CustomConfig{Script: script}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, c)
	require.Error(t, err)
}

func Test_CustomProviderGenerateTileWrongSignatureFailsAtInitialize(t *testing.T) {
	script := customScriptHeader + `
func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, cientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
)  (tilegroxy.ProviderContext, error) {
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile() {}
`

	c, err := CustomRegistration{}.Initialize(CustomConfig{Script: script}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})

	assert.Nil(t, c)
	require.Error(t, err)
}

type recordingTransport struct {
	called bool
}

func (r *recordingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	r.called = true
	return nil, errors.New("stop here")
}

func Test_CustomProviderGetTileUsesTransport(t *testing.T) {
	script := `
package custom

import "tilegroxy/tilegroxy"

func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, cientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
)  (tilegroxy.ProviderContext, error) {
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages ) (*tilegroxy.Image, error ) {
	return tilegroxy.GetTile(ctx, clientConfig, "http://example.invalid/", nil)
}
`
	transport := &recordingTransport{}

	c, err := CustomRegistration{}.Initialize(CustomConfig{Script: script}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages, Transport: transport})
	require.NoError(t, err)

	_, err = c.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{})

	require.Error(t, err)
	assert.True(t, transport.called)
}

func Test_ConstructLayer_GivesProviderResolvedTransport(t *testing.T) {
	rawConfig := config.LayerConfig{ID: "proxied", Provider: map[string]any{"name": "proxy", "url": "http://example.invalid/{z}/{x}/{y}"}}
	transport := &recordingTransport{}

	var seen config.ClientConfig

	resolve := func(c config.ClientConfig) (http.RoundTripper, error) {
		seen = c
		return transport, nil
	}

	l, err := layers.ConstructLayer(context.Background(), rawConfig, config.ClientConfig{TLS: "corp", Timeout: 1}, nil, testErrMessages, nil, nil, nil, resolve)
	require.NoError(t, err)

	_, err = l.Provider.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "proxied", Z: 1})

	require.Error(t, err)
	assert.True(t, transport.called)
	assert.Equal(t, "corp", seen.TLS)

	errResolve := errors.New("unknown profile")
	_, err = layers.ConstructLayer(context.Background(), rawConfig, config.ClientConfig{}, nil, testErrMessages, nil, nil, nil, func(config.ClientConfig) (http.RoundTripper, error) {
		return nil, errResolve
	})
	require.ErrorIs(t, err, errResolve)
}

func Test_CustomProviderLayerParams(t *testing.T) {
	script := `
func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, cientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
)  (tilegroxy.ProviderContext, error) {
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages ) (*tilegroxy.Image, error ) {
	lp := tilegroxy.LayerParams(ctx)
	service := lp["service"]
	lp["service"] = "changed"
	return &tilegroxy.Image{Content: []byte(service)}, nil
}
`
	p := buildCustomProviderFromScript(t, script)

	ctx := pkg.BackgroundContext()
	matches, ok := pkg.LayerPatternMatchesFromContext(ctx)
	require.True(t, ok)
	(*matches)["service"] = "roads"

	img, err := p.GenerateTile(ctx, layer.ProviderContext{}, pkg.TileRequest{LayerName: "svc_roads"})
	require.NoError(t, err)
	assert.Equal(t, "roads", string(img.Content))
	assert.Equal(t, "roads", (*matches)["service"])

	img, err = p.GenerateTile(context.Background(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "svc_roads"})
	require.NoError(t, err)
	assert.Empty(t, img.Content)
}
