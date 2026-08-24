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
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// existingRequiredFuncs supplies the minimum preAuth/generateTile a custom provider script needs to
// initialize, so close-specific tests don't have to restate them.
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
	// Scripts written before this existed must keep working untouched.
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
