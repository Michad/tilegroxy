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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ErrorVals_Execute(t *testing.T) {
	cfg := config.DefaultConfig()

	cfg.Error.AlwaysOK = false

	for i := pkg.TypeOfErrorBounds; i <= pkg.TypeOfErrorNotFound; i++ {
		cfg.Error.AlwaysOK = false
		status, level, imgPath, contentType := errorVars(&cfg.Error, pkg.TypeOfError(i), config.DataTypeUnknown)
		assert.Greater(t, status, 300)
		assert.NotEmpty(t, imgPath)
		assert.Equal(t, "image/png", contentType)
		cfg.Error.AlwaysOK = true
		status2, level2, imgPath2, contentType2 := errorVars(&cfg.Error, pkg.TypeOfError(i), config.DataTypeUnknown)
		assert.Equal(t, http.StatusOK, status2)
		assert.Equal(t, level2, level)
		assert.Equal(t, imgPath, imgPath2)
		assert.Equal(t, contentType, contentType2)
	}
}

func Test_ErrorVals_Mvt(t *testing.T) {
	cfg := config.DefaultConfig()

	for i := pkg.TypeOfErrorBounds; i <= pkg.TypeOfErrorNotFound; i++ {
		if pkg.TypeOfError(i) == pkg.TypeOfErrorAuth {
			continue // Auth always stays PNG, see Test_ErrorVals_Mvt_AuthAlwaysPng
		}
		_, _, imgPath, contentType := errorVars(&cfg.Error, pkg.TypeOfError(i), config.DataTypeMVT)
		assert.Equal(t, "embedded:empty.mvt", imgPath)
		assert.Equal(t, "application/vnd.mapbox-vector-tile", contentType)
	}
}

// Auth errors must never reveal a layer's data type to an unauthenticated caller, so they always
// use the raster image and image/png content type even when the layer is mvt.
func Test_ErrorVals_Mvt_AuthAlwaysPng(t *testing.T) {
	cfg := config.DefaultConfig()

	_, _, imgPath, contentType := errorVars(&cfg.Error, pkg.TypeOfErrorAuth, config.DataTypeMVT)
	assert.Equal(t, cfg.Error.Images.Authentication, imgPath)
	assert.Equal(t, "image/png", contentType)
}

// A missing layer and an authenticated-but-out-of-scope layer both use TypeOfErrorNotFound, and
// must map to 404 - see issue #766.
func Test_ErrorVals_NotFound_Returns404(t *testing.T) {
	cfg := config.DefaultConfig()

	status, _, _, _ := errorVars(&cfg.Error, pkg.TypeOfErrorNotFound, config.DataTypeUnknown)
	assert.Equal(t, http.StatusNotFound, status)
}

func Test_WriteErrorMessage_Execute(t *testing.T) {
	cfg := config.DefaultConfig()
	ctx := pkg.BackgroundContext()

	rw := httptest.NewRecorder()

	cfg.Error.Mode = config.ModeErrorNoError
	writeErrorMessage(ctx, rw, &cfg.Error, pkg.TypeOfErrorOther, "test", "test", nil, config.DataTypeUnknown)
	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, 500, r.StatusCode)
	b, _ := io.ReadAll(r.Body)
	assert.Empty(t, b)

	cfg.Error.Mode = config.ModeErrorImage
	cfg.Error.Images.Other = "safjakslfjaslkfj" // Invalid
	writeErrorMessage(ctx, rw, &cfg.Error, pkg.TypeOfErrorOther, "test", "test", nil, config.DataTypeUnknown)
	r = rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, 500, r.StatusCode)
	b, _ = io.ReadAll(r.Body)
	assert.Empty(t, b)

}

func Test_WriteErrorMessage_Mvt(t *testing.T) {
	cfg := config.DefaultConfig()
	ctx := pkg.BackgroundContext()

	rw := httptest.NewRecorder()

	cfg.Error.Mode = config.ModeErrorImage
	writeErrorMessage(ctx, rw, &cfg.Error, pkg.TypeOfErrorBounds, "test", "test", nil, config.DataTypeMVT)
	r := rw.Result()
	defer func() { require.NoError(t, r.Body.Close()) }()
	assert.Equal(t, "application/vnd.mapbox-vector-tile", r.Header.Get("Content-Type"))
}
