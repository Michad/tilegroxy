package caches

import (
	"math"
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

func validateSaveAndLookup(t *testing.T, r Cache) {
	z := rand.Float64()*10 + 5
	x := int(rand.Float64() * math.Exp2(z))
	y := int(rand.Float64() * math.Exp2(z))
	tile := internal.TileRequest{LayerName: "test", Z: int(z), X: x, Y: y}

	imgLen := rand.Int31n(100)
	img := make([]byte, imgLen)

	err := r.Save(tile, &img)
	assert.Nil(t, err)

	img2, err := r.Lookup(tile)
	assert.Nil(t, err)

	assert.True(t, slices.Equal(img, *img2), "Result before and after cache don't match")
}