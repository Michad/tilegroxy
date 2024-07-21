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

package server

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
)

func Test_ErrorVals(t *testing.T) {
	cfg := config.DefaultConfig()

	cfg.Error.AlwaysOk = false

	for i := TypeOfErrorBounds; i <= TypeOfErrorOther; i++ {
		cfg.Error.AlwaysOk = false
		status, level, imgPath := errorVars(&cfg.Error, TypeOfError(i))
		assert.Greater(t, status, 300)
		assert.NotEmpty(t, imgPath)
		cfg.Error.AlwaysOk = true
		status2, level2, imgPath2 := errorVars(&cfg.Error, TypeOfError(i))
		assert.Equal(t, 200, status2)
		assert.Equal(t, level2, level)
		assert.Equal(t, imgPath, imgPath2)
	}
}
