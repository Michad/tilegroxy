package caches

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMultiSaveAndLookup(t *testing.T) {
	memConfig1 := MemoryConfig{}

	mem1, err := ConstructMemory(memConfig1, nil)
	assert.Nil(t, err)

	memConfig2 := MemoryConfig{}

	mem2, err := ConstructMemory(memConfig2, nil)
	if !assert.Nil(t, err) {
		return
	}

	multi := Multi{Tiers: []Cache{mem1, mem2}}

	tile := makeReq(53)
	img := makeImg(24)
	multi.Save(tile, &img)

	_ = validateLookup(t, multi, tile, &img) &&
		validateLookup(t, mem1, tile, &img) &&
		validateLookup(t, mem2, tile, &img)
}

func TestMultiIn1(t *testing.T) {
	memConfig1 := MemoryConfig{}

	mem1, err := ConstructMemory(memConfig1, nil)
	assert.Nil(t, err)

	memConfig2 := MemoryConfig{}

	mem2, err := ConstructMemory(memConfig2, nil)
	if !assert.Nil(t, err) {
		return
	}

	multi := Multi{Tiers: []Cache{mem1, mem2}}

	tile := makeReq(53)
	img := makeImg(24)
	mem1.Save(tile, &img)

	validateLookup(t, multi, tile, &img)
}

func TestMultiIn2(t *testing.T) {
	memConfig1 := MemoryConfig{}

	mem1, err := ConstructMemory(memConfig1, nil)
	assert.Nil(t, err)

	memConfig2 := MemoryConfig{}

	mem2, err := ConstructMemory(memConfig2, nil)
	if !assert.Nil(t, err) {
		return
	}

	multi := Multi{Tiers: []Cache{mem1, mem2}}

	tile := makeReq(53)
	img := makeImg(24)
	mem2.Save(tile, &img)

	validateLookup(t, multi, tile, &img)
}
