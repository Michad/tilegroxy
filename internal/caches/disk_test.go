package caches

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisk(t *testing.T) {
	dir, error := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)

	if !assert.Nil(t, error) {
		return
	}

	config := DiskConfig{Path: dir}

	c, err := ConstructDisk(config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, c)
}
