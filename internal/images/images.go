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
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

const numRGB = 3
const numRGBA = 4
const defaultImageSize = 512

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

//go:embed empty.mvt
var mvtEmpty []byte

const KeyMvtEmpty = "embedded:empty.mvt"

//go:embed box.mvt
var mvtBox []byte

const KeyMvtBox = "embedded:box.mvt"

const KeyPrefixColor = "color:"

var dynamicImages = make(map[string]*[]byte, 0)
var failedImages = make(map[string]error, 0)

func parseColor(fullStr string) (color.Color, error) {
	col := fullStr[len(KeyPrefixColor):]

	if col[0:0] == "#" {
		col = col[1:]
	}

	col = strings.ToLower(col)

	colObj := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	if len(col) == numRGB {
		col = string(col[0]) + string(col[0]) + string(col[1]) + string(col[1]) + string(col[2]) + string(col[2])
	} else if len(col) == numRGBA {
		col = string(col[0]) + string(col[0]) + string(col[1]) + string(col[1]) + string(col[2]) + string(col[2]) + string(col[3]) + string(col[3])
	}

	if len(col) == 2*numRGB {
		numMatch, err := fmt.Sscanf(col, "%02x%02x%02x", &colObj.R, &colObj.G, &colObj.B)
		if numMatch != numRGB {
			return colObj, errors.New("mismatch")
		}
		return colObj, err
	}

	if len(col) == 2*numRGBA {
		numMatch, err := fmt.Sscanf(col, "%02x%02x%02x%02x", &colObj.R, &colObj.G, &colObj.B, &colObj.A)
		if numMatch != numRGBA {
			return colObj, errors.New("mismatch")
		}
		return colObj, err
	}

	return colObj, errors.New("invalid color")
}

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

	if path == KeyMvtEmpty {
		return &mvtEmpty, nil
	}

	if path == KeyMvtBox {
		return &mvtBox, nil
	}

	if dynamicImages[path] != nil {
		return dynamicImages[path], nil
	}

	if failedImages[path] != nil {
		//#nosec G404
		if rand.Float32()*100 > 1 {
			return nil, failedImages[path]
		}
	}

	if strings.Index(path, KeyPrefixColor) == 0 {
		return getColorImage(path)
	}

	img, err := os.ReadFile(filepath.Clean(path))

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

func getColorImage(path string) (*[]byte, error) {
	colObj, err := parseColor(path)

	if err != nil {
		return nil, fmt.Errorf("invalid color %v", path)
	}

	img := image.NewRGBA(image.Rect(0, 0, defaultImageSize, defaultImageSize))
	draw.Draw(img, img.Rect, image.NewUniform(colObj), img.Rect.Min, draw.Src)

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err = png.Encode(writer, img)

	if err != nil {
		return nil, err
	}

	err = writer.Flush()

	if err != nil {
		return nil, err
	}

	output := buf.Bytes()

	dynamicImages[path] = &output
	return &output, nil
}
