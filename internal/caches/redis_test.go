package caches

import (
	"context"
	"log"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/internal"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestWithContainer(t *testing.T) {
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "redis:latest",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		log.Fatalf("Could not start redis: %s", err)
	}
	defer func() {
		if err := redisC.Terminate(ctx); err != nil {
			log.Fatalf("Could not stop redis: %s", err)
		}
	}()

	endpoint, err := redisC.Endpoint(ctx, "")

	if err != nil {
		log.Fatalf("Could not start connect to redis: %s", err)
	}

	split := strings.Split(endpoint, ":")
	port, err := strconv.Atoi(split[1])

	if err != nil {
		log.Fatalf("Invalid port: %s", err)
	}

	config := RedisConfig{
		HostAndPort: HostAndPort{Host: split[0], Port: uint16(port)},
	}

	r, err := ConstructRedis(&config, nil)

	if err != nil {
		log.Fatalf("Could not create cache: %s", err)
	}

	tile := internal.TileRequest{LayerName: "test", Z: 0, X: 0, Y: 0}
	img := []byte{1, 2, 3, 4, 5}

	err = r.Save(tile, &img)

	if err != nil {
		log.Fatalf("Could not save cache: %s", err)
	}

	img2, err := r.Lookup(tile)

	if err != nil {
		log.Fatalf("Could not lookup cache: %s", err)
	}

	assert.True(t, slices.Equal(img, *img2), "Result before and after cache don't match")
}
