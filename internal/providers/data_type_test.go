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
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
)

func Test_DataType_Static(t *testing.T) {
	assert.Equal(t, pkg.DataTypeRaster, Static{}.DataType())
}

func Test_DataType_Effect(t *testing.T) {
	assert.Equal(t, pkg.DataTypeRaster, Effect{}.DataType())
}

func Test_DataType_Transform(t *testing.T) {
	assert.Equal(t, pkg.DataTypeRaster, Transform{}.DataType())
}

func Test_DataType_Blend(t *testing.T) {
	assert.Equal(t, pkg.DataTypeRaster, Blend{}.DataType())
}

func Test_DataType_CropMvt(t *testing.T) {
	assert.Equal(t, pkg.DataTypeMVT, CropMvt{}.DataType())
}

func Test_DataType_CompositeMVT(t *testing.T) {
	assert.Equal(t, pkg.DataTypeMVT, CompositeMVT{}.DataType())
}

func Test_DataType_PostgisMvt(t *testing.T) {
	assert.Equal(t, pkg.DataTypeMVT, PostgisMvt{}.DataType())
}

func Test_DataType_Proxy(t *testing.T) {
	assert.Equal(t, pkg.DataTypeUnknown, Proxy{}.DataType())
}

func Test_DataType_CGI(t *testing.T) {
	assert.Equal(t, pkg.DataTypeUnknown, CGI{}.DataType())
}

func Test_DataType_Custom(t *testing.T) {
	assert.Equal(t, pkg.DataTypeUnknown, Custom{}.DataType())
}

func Test_DataType_Fail(t *testing.T) {
	assert.Equal(t, pkg.DataTypeUnknown, Fail{}.DataType())
}

func Test_DataType_Ref(t *testing.T) {
	assert.Equal(t, pkg.DataTypeUnknown, Ref{}.DataType())
}

func Test_DataType_Fallback_PassesThroughPrimary(t *testing.T) {
	f := Fallback{Primary: fixedDataTypeTestProvider{pkg.DataTypeMVT}}
	assert.Equal(t, pkg.DataTypeMVT, f.DataType())
}

func Test_DataType_Crop_PassesThroughPrimary(t *testing.T) {
	c := Crop{Primary: fixedDataTypeTestProvider{pkg.DataTypeRaster}}
	assert.Equal(t, pkg.DataTypeRaster, c.DataType())
}

// URLTemplate embeds Proxy, so it inherits DataType() without its own method.
func Test_DataType_URLTemplate_InheritsFromProxy(t *testing.T) {
	u := URLTemplate{}
	assert.Equal(t, pkg.DataTypeUnknown, u.DataType())
}

type fixedDataTypeTestProvider struct {
	dt pkg.DataType
}

func (p fixedDataTypeTestProvider) PreAuth(_ context.Context, pc layer.ProviderContext) (layer.ProviderContext, error) {
	return pc, nil
}

func (p fixedDataTypeTestProvider) GenerateTile(_ context.Context, _ layer.ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (p fixedDataTypeTestProvider) DataType() pkg.DataType {
	return p.dt
}
