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

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_HandleFavicon_ServesSVG(t *testing.T) {
	w := httptest.NewRecorder()

	handleFavicon(w, httptest.NewRequest(http.MethodGet, "http://internal-host/favicon.svg", nil))

	res := w.Result()
	defer func() { require.NoError(t, res.Body.Close()) }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "image/svg+xml", res.Header.Get("Content-Type"))
	assert.Equal(t, favicon, body)
}
