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

package images

import (
	_ "embed"
	"errors"
	"math/rand"
	"os"
)

//go:embed error.png
var imageError []byte

const KeyImageError = "embedded:error.png"

//go:embed red.png
var imageRed []byte

const KeyImageRed = "embedded:red.png"

//go:embed transparent.png
var imageTransparent []byte

const KeyImageTransparent = "embedded:transparent.png"

//go:embed unauthorized.png
var imageUnauthorized []byte

const KeyImageUnauthorized = "embedded:unauthorized.png"

var dynamicImages = make(map[string]*[]byte, 0)
var failedImages = make(map[string]error, 0)

// Returns the contents of an image. This can be an embedded image if path starts with "embedded:". The path will be treated as a standard filepath otherwise.  The contents of the image will be permanently cached in memory, this should be only used for images that will be reused a lot such as error responses. Errors will also be cached but 1% of the time it will be retried.
func GetStaticImage(path string) (*[]byte, error) {
	if path == KeyImageError {
		return &imageError, nil
	}

	if path == KeyImageRed {
		return &imageRed, nil
	}

	if path == KeyImageTransparent {
		return &imageTransparent, nil
	}

	if path == KeyImageUnauthorized {
		return &imageUnauthorized, nil
	}

	if dynamicImages[path] != nil {
		return dynamicImages[path], nil
	}

	if failedImages[path] != nil {
		if rand.Float32()*100 > 1 {
			return nil, failedImages[path]
		}
	}

	img, err := os.ReadFile(path)

	if img != nil {
		dynamicImages[path] = &img
		return &img, nil
	}

	if err != nil {
		failedImages[path] = err
		return nil, err
	}

	return nil, errors.New("image failed to load")
}
