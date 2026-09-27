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

package pmtiles

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
)

func Test_MissingTileError(t *testing.T) {
	err := MissingTileError{Z: 3, X: 1, Y: 2}

	var typed pkg.TypedError = err
	assert.Equal(t, pkg.TypeOfError(pkg.TypeOfErrorBounds), typed.Type())
	assert.Equal(t, "missing 3/1/2", typed.External(config.ErrorMessages{TileNotFound: "missing %v"}))
	assert.Contains(t, err.Error(), "3/1/2")
}
