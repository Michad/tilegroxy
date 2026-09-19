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

func Test_ReadDocumentationFile_DirectoryHandling(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		expectedContent bool
		expectedMIME    string
	}{
		{
			name:            "directory without trailing slash redirects to index",
			path:            "decisions",
			expectedContent: true,
			expectedMIME:    "text/html; charset=utf-8",
		},
		{
			name:            "directory with trailing slash serves index",
			path:            "decisions/",
			expectedContent: true,
			expectedMIME:    "text/html; charset=utf-8",
		},
		{
			name:            "directory with leading slash and no trailing slash redirects",
			path:            "/operation",
			expectedContent: true,
			expectedMIME:    "text/html; charset=utf-8",
		},
		{
			name:            "directory with both leading and trailing slash serves index",
			path:            "/development/",
			expectedContent: true,
			expectedMIME:    "text/html; charset=utf-8",
		},
		{
			name:            "root path serves index.html",
			path:            "/",
			expectedContent: true,
			expectedMIME:    "text/html; charset=utf-8",
		},
		{
			name:            "empty path serves index.html",
			path:            "",
			expectedContent: true,
			expectedMIME:    "text/html; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, mime, err := ReadDocumentationFile(tt.path)
			require.NoError(t, err, "unexpected error for path %q", tt.path)
			assert.NotEmpty(t, data, "expected content for path %q", tt.path)
			assert.Equal(t, tt.expectedMIME, mime, "MIME type mismatch for path %q", tt.path)
		})
	}
}

func Test_ReadDocumentationFile_DirectoryRecursion(t *testing.T) {
	decisions, mime, err := ReadDocumentationFile("decisions")
	require.NoError(t, err)

	decisionsWithSlash, mimeWithSlash, err := ReadDocumentationFile("decisions/")
	require.NoError(t, err)

	assert.Equal(t, decisions, decisionsWithSlash, "directory handling should be consistent")
	assert.Equal(t, mime, mimeWithSlash, "MIME type should be consistent")
}

func Test_ReadDocumentationFile_FilesServeDirectly(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		expectedMIME string
	}{
		{
			name:         "HTML file",
			path:         "decisions/0000-template.html",
			expectedMIME: "text/html; charset=utf-8",
		},
		{
			name:         "file with leading slash",
			path:         "/operation/configuration.html",
			expectedMIME: "text/html; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, mime, err := ReadDocumentationFile(tt.path)
			require.NoError(t, err)
			assert.NotEmpty(t, data)
			assert.Equal(t, tt.expectedMIME, mime)
		})
	}
}
