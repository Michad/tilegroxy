// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package images

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColors(t *testing.T) {
	col, err := parseColor(KeyPrefixColor + "FFF")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 255, 255, 255})

	col, err = parseColor(KeyPrefixColor + "fff")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 255, 255, 255})

	col, err = parseColor(KeyPrefixColor + "FFFFFF")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 255, 255, 255})

	col, err = parseColor(KeyPrefixColor + "ffffff")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 255, 255, 255})

	col, err = parseColor(KeyPrefixColor + "FFFF")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 255, 255, 255})

	col, err = parseColor(KeyPrefixColor + "ffff")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 255, 255, 255})

	col, err = parseColor(KeyPrefixColor + "ffffffff")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 255, 255, 255})

	col, err = parseColor(KeyPrefixColor + "f01")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{255, 0, 17, 255})

	col, err = parseColor(KeyPrefixColor + "aaaa")

	assert.NoError(t, err)
	assert.Equal(t, col, color.RGBA{0xaa, 0xaa, 0xaa, 0xaa})

	_, err = parseColor(KeyPrefixColor + "hello")
	assert.Error(t, err)

	_, err = parseColor(KeyPrefixColor + "ffffffffff")
	assert.Error(t, err)

	_, err = parseColor(KeyPrefixColor + "")
	assert.Error(t, err)

}

func TestImageLoad(t *testing.T) {
	img, err := GetStaticImage("error.png")
	assert.NotNil(t, img)
	assert.NoError(t, err)

	assert.Equal(t, imageError, *img)
}
