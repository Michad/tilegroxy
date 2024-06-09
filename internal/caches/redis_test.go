package caches

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupRedisContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
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
	if !assert.Nil(t, err) {
		return nil, nil
	}

	return redisC, func(t *testing.T) {
		t.Log("teardown container")

		err := redisC.Terminate(ctx)
		assert.Nil(t, err)
	}
}

func TestRedisWithContainerHostAndPort(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := ConstructRedis(&config, nil)
	if !assert.Nil(t, err) {
		return
	}

	validateSaveAndLookup(t, r)
}

func TestRedisWithContainerSingleServersArr(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := RedisConfig{
		Servers: []HostAndPort{extractHostAndPort(t, endpoint)},
	}

	r, err := ConstructRedis(&config, nil)
	if !assert.Nil(t, err) {
		return
	}

	validateSaveAndLookup(t, r)
}

func TestRedisWithContainerRing(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	redisC2, cleanupF2 := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) || !assert.NotNil(t, redisC2) {
		return
	}

	defer cleanupF(t)
	defer cleanupF2(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}
	endpoint2, err := redisC2.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := RedisConfig{
		Mode:    ModeRing,
		Servers: []HostAndPort{extractHostAndPort(t, endpoint), extractHostAndPort(t, endpoint2)},
	}

	r, err := ConstructRedis(&config, nil)
	if !assert.Nil(t, err) {
		return
	}

	validateSaveAndLookup(t, r)
}

func TestRedisWithContainerDiffPrefix(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "first_",
	}

	r, err := ConstructRedis(&config, nil)
	if !assert.Nil(t, err) {
		return
	}

	config2 := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "second_",
	}

	r2, err := ConstructRedis(&config2, nil)
	if !assert.Nil(t, err) {
		return
	}

	_ = validateSaveAndLookup(t, r) &&
		validateSaveAndLookup(t, r2)
}
func TestRedisWithContainerDiffDb(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		Db:          0,
	}

	r, err := ConstructRedis(&config, nil)
	if !assert.Nil(t, err) {
		return
	}

	config2 := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		Db:          1,
	}

	r2, err := ConstructRedis(&config2, nil)
	_ = assert.Nil(t, err) &&
		validateSaveAndLookup(t, r) &&
		validateSaveAndLookup(t, r2)
}
