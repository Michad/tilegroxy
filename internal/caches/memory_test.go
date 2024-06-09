package caches

import (
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/stretchr/testify/assert"
)

func TestMemory(t *testing.T) {
	config := MemoryConfig{}

	r, err := ConstructMemory(config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
}

func TestTtl(t *testing.T) {
	config := MemoryConfig{Ttl: 1}

	r, err := ConstructMemory(config, nil)
	assert.Nil(t, err)

	tile := makeReq(53)
	img := makeImg(53)

	r.Save(tile, &img)

	validateLookup(t, r, tile, &img)
	time.Sleep(time.Duration(2) * time.Second)
	validateNoLookup(t, r, tile)
}

func TestMemoryLimit(t *testing.T) {
	config := MemoryConfig{MaxSize: 10}
	nToTest := 150 //This must be sufficiently higher than MaxSize to ensure the first entry is evicted

	mem, err := ConstructMemory(config, nil)
	assert.Nil(t, err)

	tiles := make([]internal.TileRequest, 0)
	images := make([]internal.Image, 0)
	for i := range nToTest {
		tiles = append(tiles, makeReq(i))
		images = append(images, makeImg(i))
	}

	mem.Save(tiles[0], &images[0])

	validateLookup(t, mem, tiles[0], &images[0])

	for i := range nToTest - 1 {
		mem.Save(tiles[i+1], &images[i+1])
	}

	validateNoLookup(t, mem, tiles[0])
}
