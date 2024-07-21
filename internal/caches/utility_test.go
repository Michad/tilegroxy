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

package caches

import (
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/assert"
)

func TestHostAndPortToString(t *testing.T) {
	hp := HostAndPort{"127.0.0.1", uint16(1234)}
	assert.Equal(t, "127.0.0.1:1234", hp.String())
}

func TestHostAndPortToStringArr(t *testing.T) {
	hp := HostAndPort{"127.0.0.1", uint16(1234)}
	hp2 := HostAndPort{"10.0.0.1", uint16(5678)}

	assert.Equal(t, []string{"127.0.0.1:1234", "10.0.0.1:5678"}, HostAndPortArrayToStringArray([]HostAndPort{hp, hp2}))
}

/*** Utility methods used in most other cache tests ***/

func extractHostAndPort(t *testing.T, endpoint string) HostAndPort {
	split := strings.Split(endpoint, ":")
	port, err := strconv.Atoi(split[1])
	assert.NoError(t, err)

	return HostAndPort{Host: split[0], Port: uint16(port)}
}

func makeReq(seed int) pkg.TileRequest {
	z := 20
	x := seed
	y := seed
	return pkg.TileRequest{LayerName: "test", Z: int(z), X: x, Y: y}
}

func makeImg(seed int) pkg.Image {
	return make([]byte, seed)
}

func validateSaveAndLookup(t *testing.T, c cache.Cache) bool {
	//TODO: reconsider use of rand
	tile := makeReq(rand.Intn(10000))
	img := makeImg(rand.Intn(100))

	err := c.Save(tile, &img)
	if !assert.Nil(t, err, "Cache save returned an error") {
		return false
	}

	return validateLookup(t, c, tile, &img)
}

func validateLookup(t *testing.T, c cache.Cache, tile pkg.TileRequest, expected *pkg.Image) bool {
	img2, err := c.Lookup(tile)
	if !assert.Nil(t, err, "Cache lookup returned an error") {
		return false
	}
	if !assert.NotNil(t, img2, "Cache lookup didn't return an image") {
		return false
	}

	return assert.True(t, slices.Equal(*expected, *img2), "Result before and after cache don't match")
}

func validateNoLookup(t *testing.T, c cache.Cache, tile pkg.TileRequest) bool {
	img2, err := c.Lookup(tile)
	if !assert.Nil(t, err, "Cache lookup returned an error") {
		return false
	}
	if !assert.Nil(t, img2, "Cache lookup returned a result when it shouldn't") {
		return false
	}
	return true
}
