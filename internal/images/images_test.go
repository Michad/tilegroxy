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
	"github.com/stretchr/testify/require"
)

func TestColors(t *testing.T) {
	col, err := parseColor(KeyPrefixColor + "FFF")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 255, 255, 255}, col)

	col, err = parseColor(KeyPrefixColor + "fff")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 255, 255, 255}, col)

	col, err = parseColor(KeyPrefixColor + "FFFFFF")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 255, 255, 255}, col)

	col, err = parseColor(KeyPrefixColor + "ffffff")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 255, 255, 255}, col)

	col, err = parseColor(KeyPrefixColor + "FFFF")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 255, 255, 255}, col)

	col, err = parseColor(KeyPrefixColor + "ffff")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 255, 255, 255}, col)

	col, err = parseColor(KeyPrefixColor + "ffffffff")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 255, 255, 255}, col)

	col, err = parseColor(KeyPrefixColor + "f01")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 0, 17, 255}, col)

	col, err = parseColor(KeyPrefixColor + "aaaa")

	require.NoError(t, err)
	assert.Equal(t, color.RGBA{0xaa, 0xaa, 0xaa, 0xaa}, col)

	_, err = parseColor(KeyPrefixColor + "hello")
	require.Error(t, err)

	_, err = parseColor(KeyPrefixColor + "ffffffffff")
	require.Error(t, err)

	_, err = parseColor(KeyPrefixColor + "")
	require.Error(t, err)

}

func TestImageLoad(t *testing.T) {
	img, err := GetStaticImage("error.png")
	assert.NotNil(t, img)
	require.NoError(t, err)

	assert.Equal(t, imageError, *img)
}
