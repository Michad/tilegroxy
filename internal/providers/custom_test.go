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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/stretchr/testify/assert"
)

func Test_CustomValidate(t *testing.T) {
	c, err := ConstructCustom(CustomConfig{}, nil, &testErrMessages)

	assert.Nil(t, c)
	assert.Error(t, err)

	c, err = ConstructCustom(CustomConfig{Script: "package custom"}, nil, &testErrMessages)

	assert.Nil(t, c)
	assert.Error(t, err)
}

func Test_CustomExecute(t *testing.T) {
	c, err := ConstructCustom(CustomConfig{Script: `
package custom

import (
	"math/rand"
	"strconv"
	"strings"

	"tilegroxy/tilegroxy"
)
func preAuth(ctx *tilegroxy.RequestContext, providerContext tilegroxy.ProviderContext, params map[string]interface{}, cientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
)  (tilegroxy.ProviderContext, error) {
	return tilegroxy.ProviderContext{AuthBypass: true}, nil
}

func generateTile(ctx *tilegroxy.RequestContext, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages ) (*tilegroxy.Image, error ) {
	return &[]byte{0x01,0x02}, nil
}
	`}, &config.ClientConfig{}, &testErrMessages)

	if err != nil {
		fmt.Println(err)
	}

	assert.NotNil(t, c)
	assert.NoError(t, err)

	pc, err := c.PreAuth(internal.BackgroundContext(), ProviderContext{})
	assert.NoError(t, err)
	assert.NotNil(t, pc)
	assert.True(t, pc.AuthBypass)

	img, err := c.GenerateTile(internal.BackgroundContext(), pc, internal.TileRequest{LayerName: "l", Z: 3, X: 1, Y: 2})
	assert.NoError(t, err)
	assert.NotNil(t, img)
	assert.Equal(t, []byte{0x01, 0x02}, *img)

}
