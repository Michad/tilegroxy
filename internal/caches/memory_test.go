package caches

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemory(t *testing.T) {
	config := MemoryConfig{}

	r, err := ConstructMemory(config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
}
