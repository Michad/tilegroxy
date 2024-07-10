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

package layers

import (
	"testing"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/stretchr/testify/assert"
)

func makeTransformProvider() Provider {
	p, _ := ConstructStatic(StaticConfig{Color: "F00"}, testClientConfig, testErrMessages)
	return *p
}

func Test_Transform_Validate(t *testing.T) {
	p := makeTransformProvider()
	tr, err := ConstructTransform(TransformConfig{}, testClientConfig, testErrMessages, p)

	assert.Nil(t, tr)
	assert.Error(t, err)
	tr, err = ConstructTransform(TransformConfig{Formula: "package custom"}, testClientConfig, testErrMessages, p)

	assert.Nil(t, tr)
	assert.Error(t, err)
}

func Test_Transform_Execute(t *testing.T) {
	p := makeTransformProvider()
	tr, err := ConstructTransform(TransformConfig{Formula: `func transform(r, g, b, a uint8) (uint8, uint8, uint8, uint8) { return g,b,r,a }`}, testClientConfig, testErrMessages, p)

	assert.NotNil(t, tr)
	assert.NoError(t, err)

	exp, _ := images.GetStaticImage("color:00F")

	pc, err := tr.PreAuth(internal.BackgroundContext(), ProviderContext{})
	assert.NotNil(t, pc)
	assert.NoError(t, err)

	img, err := tr.GenerateTile(internal.BackgroundContext(), pc, internal.TileRequest{LayerName: "l", Z: 9, X: 23, Y: 32})

	assert.NoError(t, err)
	assert.Equal(t, *exp, *img)
}
