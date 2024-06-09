package caches

import (
	"testing"
	"time"

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

	if !validateLookup(t, r, tile, &img) {
		return
	}
	time.Sleep(time.Duration(2) * time.Second)
	validateNoLookup(t, r, tile)
}

//We intentionally don't test the maxsize property as the otter library doesn't offer guarantees on how capacity settings are honored.  See https://github.com/maypok86/otter/issues/88 for more details
