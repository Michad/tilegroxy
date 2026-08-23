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

package layer

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
)

type fixedDataTypeProvider struct {
	dataType pkg.DataType
}

func (p fixedDataTypeProvider) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	return providerContext, nil
}

func (p fixedDataTypeProvider) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (p fixedDataTypeProvider) DataType() pkg.DataType {
	return p.dataType
}

func Test_ProviderWrapper_DataType_ForwardsToWrapped(t *testing.T) {
	w := ProviderWrapper{Name: "test", Provider: fixedDataTypeProvider{dataType: pkg.DataTypeMVT}}

	assert.Equal(t, pkg.DataTypeMVT, w.DataType())
}
