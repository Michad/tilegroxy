package caches

import (
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/internal"
	"github.com/stretchr/testify/assert"
)

func extractHostAndPort(t *testing.T, endpoint string) HostAndPort {
	split := strings.Split(endpoint, ":")
	port, err := strconv.Atoi(split[1])
	assert.Nil(t, err)

	return HostAndPort{Host: split[0], Port: uint16(port)}
}

func makeReq(seed int) internal.TileRequest {
	z := 20
	x := seed
	y := seed
	return internal.TileRequest{LayerName: "test", Z: int(z), X: x, Y: y}
}

func makeImg(seed int) internal.Image {
	return make([]byte, seed)
}

func validateSaveAndLookup(t *testing.T, c Cache) {
	//TODO: reconsider use of rand
	tile := makeReq(rand.Intn(10000))
	img := makeImg(rand.Intn(100))

	err := c.Save(tile, &img)
	assert.Nil(t, err, "Cache save returned an error")

	validateLookup(t, c, tile, &img)
}

func validateLookup(t *testing.T, c Cache, tile internal.TileRequest, expected *internal.Image) {
	img2, err := c.Lookup(tile)
	assert.Nil(t, err, "Cache lookup returned an error")
	assert.NotNil(t, img2, "Cache lookup didn't return an image")

	assert.True(t, slices.Equal(*expected, *img2), "Result before and after cache don't match")
}

func validateNoLookup(t *testing.T, c Cache, tile internal.TileRequest) {
	img2, err := c.Lookup(tile)
	assert.Nil(t, err, "Cache lookup returned an error")
	assert.Nil(t, img2, "Cache lookup returned a result when it shouldn't")
}
