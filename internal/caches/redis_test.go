package caches

import (
	"context"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/internal"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
	t.Log("setup container")

	req := testcontainers.ContainerRequest{
		Image:        "redis:latest",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	assert.Nil(t, err)

	return redisC, func(t *testing.T) {
		t.Log("teardown container")

		err := redisC.Terminate(ctx)
		assert.Nil(t, err)
	}
}

func extractHostAndPort(t *testing.T, endpoint string) HostAndPort {
	split := strings.Split(endpoint, ":")
	port, err := strconv.Atoi(split[1])
	assert.Nil(t, err)

	return HostAndPort{Host: split[0], Port: uint16(port)}
}

func validateSaveAndLookup(t *testing.T, r *Redis) {

	z := rand.Float64()*10 + 5

	x := int(rand.Float64() * math.Exp2(z))
	y := int(rand.Float64() * math.Exp2(z))
	tile := internal.TileRequest{LayerName: "test", Z: int(z), X: x, Y: y}
	img := []byte{1, 2, 3, 4, 5}

	err := r.Save(tile, &img)
	assert.Nil(t, err)

	img2, err := r.Lookup(tile)
	assert.Nil(t, err)

	assert.True(t, slices.Equal(img, *img2), "Result before and after cache don't match")
}

func TestWithContainerHostAndPort(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupContainer(ctx, t)

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	assert.Nil(t, err)

	config := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := ConstructRedis(&config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
}

func TestWithContainerSingleServersArr(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupContainer(ctx, t)

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	assert.Nil(t, err)

	config := RedisConfig{
		Servers: []HostAndPort{extractHostAndPort(t, endpoint)},
	}

	r, err := ConstructRedis(&config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
}

func TestWithContainerRing(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupContainer(ctx, t)
	redisC2, cleanupF2 := setupContainer(ctx, t)

	defer cleanupF(t)
	defer cleanupF2(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	assert.Nil(t, err)
	endpoint2, err := redisC2.Endpoint(ctx, "")
	assert.Nil(t, err)

	config := RedisConfig{
		Mode:    ModeRing,
		Servers: []HostAndPort{extractHostAndPort(t, endpoint), extractHostAndPort(t, endpoint2)},
	}

	r, err := ConstructRedis(&config, nil)
	assert.Nil(t, err)

	validateSaveAndLookup(t, r)
}
