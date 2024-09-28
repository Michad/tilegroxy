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

package website

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ReadDocumentationFile(t *testing.T) {
	index1, contentType1, err := ReadDocumentationFile("index.html")
	require.NoError(t, err)
	index2, contentType2, err := ReadDocumentationFile("")
	require.NoError(t, err)
	index3, contentType3, err := ReadDocumentationFile("/")
	require.NoError(t, err)

	assert.NotEmpty(t, index1)
	assert.Equal(t, index1, index2)
	assert.Equal(t, index1, index3)
	assert.Equal(t, "text/html; charset=utf-8", contentType1)
	assert.Equal(t, contentType1, contentType2)
	assert.Equal(t, contentType1, contentType3)

	fake, ext, err := ReadDocumentationFile("alkfjasfkjasflk")
	require.Error(t, err)
	assert.Empty(t, ext)
	assert.Nil(t, fake)
}
