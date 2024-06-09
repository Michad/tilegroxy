package caches

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
)

func setupMemcacheContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
	t.Log("setup container")

	req := testcontainers.ContainerRequest{
		Image:        "memcached:latest",
		ExposedPorts: []string{"11211/tcp"},
		// WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	memcacheC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	assert.Nil(t, err)

	return memcacheC, func(t *testing.T) {
		t.Log("teardown container")

		err := memcacheC.Terminate(ctx)
		assert.Nil(t, err)
	}
}

func TestMemcacheWithContainerHostAndPort(t *testing.T) {
	ctx := context.Background()
	memcacheC, cleanupF := setupMemcacheContainer(ctx, t)

	defer cleanupF(t)

	endpoint, err := memcacheC.Endpoint(ctx, "")
	assert.Nil(t, err)

	config := MemcacheConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := ConstructMemcache(&config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
}

func TestMemcacheWithContainerSingleServersArr(t *testing.T) {
	ctx := context.Background()
	memcacheC, cleanupF := setupMemcacheContainer(ctx, t)

	defer cleanupF(t)

	endpoint, err := memcacheC.Endpoint(ctx, "")
	assert.Nil(t, err)

	config := MemcacheConfig{
		Servers: []HostAndPort{extractHostAndPort(t, endpoint)},
	}

	r, err := ConstructMemcache(&config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
}

func TestMemcacheWithContainerDiffPrefix(t *testing.T) {
	ctx := context.Background()
	memcacheC, cleanupF := setupMemcacheContainer(ctx, t)

	defer cleanupF(t)

	endpoint, err := memcacheC.Endpoint(ctx, "")
	assert.Nil(t, err)

	config := MemcacheConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "first_",
	}

	r, err := ConstructMemcache(&config, nil)
	assert.Nil(t, err)

	config2 := MemcacheConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "second_",
	}

	r2, err := ConstructMemcache(&config2, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
	validateSaveAndLookup(t, r2)
}
