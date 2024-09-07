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
	"embed"
	"errors"
	"io/fs"
	"mime"
	"path/filepath"
)

var (
	//go:embed resources/*
	files embed.FS
)

const index = "index.html"
const indexFallback = "index_default.html"

func ReadDocumentationFile(path string) ([]byte, string, error) {
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	if path == "" {
		path = index
	}

	filePath := "resources/" + path
	data, err := files.ReadFile(filePath)

	if err != nil {
		if errors.Is(err, errors.New("is a directory")) {
			if path[len(path)-1] != '/' {
				path += "/"
			}

			path += index

			return ReadDocumentationFile(path)
		} else if path == index && errors.Is(err, fs.ErrNotExist) {
			return ReadDocumentationFile(indexFallback)
		}

		return nil, "", err
	}

	ext := mime.TypeByExtension(filepath.Ext(filePath))

	return data, ext, nil
}
