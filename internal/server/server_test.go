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
	"io"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ErrorVals_Execute(t *testing.T) {
	cfg := config.DefaultConfig()

	cfg.Error.AlwaysOk = false

	for i := pkg.TypeOfErrorBounds; i <= pkg.TypeOfErrorOther; i++ {
		cfg.Error.AlwaysOk = false
		status, level, imgPath := errorVars(&cfg.Error, pkg.TypeOfError(i))
		assert.Greater(t, status, 300)
		assert.NotEmpty(t, imgPath)
		cfg.Error.AlwaysOk = true
		status2, level2, imgPath2 := errorVars(&cfg.Error, pkg.TypeOfError(i))
		assert.Equal(t, 200, status2)
		assert.Equal(t, level2, level)
		assert.Equal(t, imgPath, imgPath2)
	}
}

func Test_WriteErrorMessage_Execute(t *testing.T) {
	cfg := config.DefaultConfig()
	ctx := pkg.BackgroundContext()

	rw := httptest.NewRecorder()

	cfg.Error.Mode = config.ModeErrorNoError
	writeErrorMessage(ctx, rw, &cfg.Error, pkg.TypeOfErrorOther, "test", "test", nil)
	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, 500, r.StatusCode)
	b, _ := io.ReadAll(r.Body)
	assert.Empty(t, b)

	cfg.Error.Mode = config.ModeErrorImage
	cfg.Error.Images.Other = "safjakslfjaslkfj" // Invalid
	writeErrorMessage(ctx, rw, &cfg.Error, pkg.TypeOfErrorOther, "test", "test", nil)
	r = rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, 500, r.StatusCode)
	b, _ = io.ReadAll(r.Body)
	assert.Empty(t, b)

}

func Test_ListenAndServe_Validate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Encrypt = &config.EncryptionConfig{Certificate: "asfjaslkf", Domain: ""}

	require.Error(t, ListenAndServe(&cfg, nil, nil))
}
