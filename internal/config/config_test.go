package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSimpleYml(t *testing.T) {
	c, err := LoadConfigFromFile("../../examples/configurations/simple.yml")

	assert.Equal(t, nil, err)
	assert.Equal(t, "Test", c.Cache["name"])
}
